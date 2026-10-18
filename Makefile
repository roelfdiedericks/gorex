# GoRex's builds. Development builds are a plain go build in dev/, with the
# files GoRex looks for beside its executable copied there when they
# change; the remote payloads and ConPTY are rebuilt or fetched only when
# their inputs change; the installer is made only by make release, with
# mygo build, into the out directory of mygo.json.
#
#   make dev          GoRex in dev/ (no payloads rebuilt, no installer)
#   make dev-remote   dev, plus the payloads of REMOTE (default linux/amd64)
#   make run          dev, and run it
#   make payloads     the payloads of every host platform
#   make release      everything, as shipped: payloads, ConPTY, installer
#   make test, vet, clean

DEV    := dev
REMOTE ?= linux/amd64

# The app's name, version and identifier, from mygo.json, which mygo build
# links in too.
json   = $(shell sed -n 's/^ *"$(1)": *"\([^"]*\)".*/\1/p' mygo.json)
NAME   := $(call json,name)
EXE    := $(DEV)/$(NAME).exe
LDFLAGS := -H=windowsgui \
	-X 'github.com/egoist/mygo.packageName=$(NAME)' \
	-X 'github.com/egoist/mygo.packageVersion=$(call json,version)' \
	-X 'github.com/egoist/mygo.packageIdentifier=$(call json,identifier)'

# What the app is built from: its Go code and embedded assets, and MyGo's
# code where go.mod replaces MyGo with a checkout. rwildcard walks a tree
# with make alone: find is Windows' own on many a PATH.
rwildcard = $(foreach d,$(wildcard $(1:=/*)),$(call rwildcard,$(d),$(2)) $(filter $(2),$(d)))
MYGO_DIR := $(shell sed -n 's/^replace github.com\/egoist\/mygo => \(\.[^ ]*\)$$/\1/p' go.mod)
APP_SRC  := $(filter-out ./$(DEV)/% ./build/%,$(call rwildcard,.,%.go)) \
	$(call rwildcard,assets,%) go.mod go.sum
MYGO_SRC := $(if $(MYGO_DIR),$(filter-out %_test.go,$(call rwildcard,$(MYGO_DIR),%.go)))
APP_DEPS := $(APP_SRC) $(MYGO_SRC)

PAYLOADS    := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64
WANT_REMOTE := $(subst /,-,$(REMOTE))
CONPTY      := resources/windows-amd64/conpty.dll resources/windows-amd64/x64/OpenConsole.exe

.PHONY: dev dev-remote run payloads release test vet clean

dev: $(EXE) $(DEV)/conpty.dll $(DEV)/x64/OpenConsole.exe $(DEV)/conpty-LICENSE.txt $(DEV)/icon.png

dev-remote: dev $(WANT_REMOTE:%=$(DEV)/remote/%.tar.gz)

run: dev
	./$(EXE)

$(EXE): $(APP_DEPS)
	go build -o $@ -ldflags "$(LDFLAGS)" .

# A host platform's payload is GoRex's server built for it: any change to
# the code makes it stale.
resources/remote/%.tar.gz: $(APP_DEPS) tools/remotepayload/main.go
	go run ./tools/remotepayload -targets $(subst -,/,$*)

payloads: $(PAYLOADS:%=resources/remote/%.tar.gz)

# ConPTY is fetched once, and again when the tool, which pins its version,
# changes.
$(CONPTY) &: tools/conpty/main.go
	go run ./tools/conpty

# Copied beside the dev executable when newer.
$(DEV)/%: resources/windows-amd64/%
	@mkdir -p $(@D) && cp -p $< $@
$(DEV)/remote/%: resources/remote/%
	@mkdir -p $(@D) && cp -p $< $@
$(DEV)/icon.png: resources/icon.png
	@mkdir -p $(@D) && cp -p $< $@

# mygo.json's buildCommand builds every payload; make has done so already.
release: payloads $(CONPTY)
	go tool mygo build --skip-build-command

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf $(DEV)
