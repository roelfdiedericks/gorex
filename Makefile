# GoRex's builds.
#
#   make dev       GoRex for this machine's platform in dev/, with the
#                  servers it copies to ssh hosts in dev/remote, one per
#                  platform in PAYLOADS; go build decides what changed
#   make run       dev, and run it
#   make release   go tool mygo build: every server, the installer
#   make test, vet, clean

# The app and the servers build side by side.
MAKEFLAGS += -j

DEV      := dev
PAYLOADS ?= linux/amd64,linux/arm64,darwin/amd64,darwin/arm64

# This machine's platform, as Go names it, e.g. windows-amd64.
GOENV    := $(shell go env GOOS GOARCH)
GOOS     := $(word 1,$(GOENV))
PLATFORM := $(GOOS)-$(word 2,$(GOENV))

# The app's name, version and identifier, from mygo.json, which mygo build
# links in too.
json    = $(shell sed -n 's/^ *"$(1)": *"\([^"]*\)".*/\1/p' mygo.json)
NAME    := $(call json,name)
VERSION := $(call json,version)
IDENT   := $(call json,identifier)
EXE     := $(DEV)/$(NAME)$(if $(filter windows,$(GOOS)),.exe)
LDFLAGS := $(if $(filter windows,$(GOOS)),-H=windowsgui) \
	-X 'github.com/egoist/mygo.packageName=$(NAME)' \
	-X 'github.com/egoist/mygo.packageVersion=$(VERSION)' \
	-X 'github.com/egoist/mygo.packageIdentifier=$(IDENT)'

# ConPTY, Windows' pseudoconsole that GoRex runs its sessions in, is
# fetched by tools/conpty, which pins its version.
CONPTY := resources/windows-amd64/conpty.dll resources/windows-amd64/x64/OpenConsole.exe

# The resources of this platform, and those of every platform but the
# servers, which dev builds itself.
rfiles   = $(foreach f,$(wildcard $(1)/*),$(if $(wildcard $(f)/.),$(call rfiles,$(f)),$(f)))
PLAT_RES := $(patsubst resources/$(PLATFORM)/%,$(DEV)/%,$(sort $(call rfiles,resources/$(PLATFORM)) \
	$(if $(filter windows-amd64,$(PLATFORM)),$(CONPTY))))
TOP_RES  := $(patsubst resources/%,$(DEV)/%,$(filter-out resources/remote resources/%-amd64 resources/%-arm64,$(wildcard resources/*)))

# Every step says why it runs, the command, and how long it took; each
# goal ends with the time since make started. The clock is GNU date's, or
# Perl's on macOS, whose date has no milliseconds.
comma := ,
now   := $(if $(filter darwin,$(GOOS)),perl -MTime::HiRes=time -e 'printf "%d\n"$(comma) time*1000',date +%s%3N)
START := $(shell $(now))

define run
$(info [make] $@: $(if $(filter FORCE,$^),go build decides what changed,$(if $?,$(words $?) of $(words $^) prerequisites newer$(comma) first $(firstword $?),it does not exist or is phony)))
$(info [make] $@: $(1))
@t=$$($(now)); $(1); s=$$?; echo "[make] $@: $$(( $$($(now)) - t )) ms$$( [ $$s -ne 0 ] && echo "$(comma) failed ($$s)")"; exit $$s
endef

total = @echo "[make] $@: total $$(( $$($(now)) - $(START) )) ms since make started"

$(info [make] goals: $(or $(MAKECMDGOALS),dev) (make $(MAKE_VERSION), $(PLATFORM)))
$(info [make] app: $(NAME) $(VERSION) ($(IDENT)) -> $(EXE))

.PHONY: dev run release test vet clean FORCE

dev: $(EXE) $(DEV)/remote $(PLAT_RES) $(TOP_RES)
	$(total)

run: dev
	$(call run,./$(EXE))

$(EXE): FORCE
	$(call run,go build -o $@ -ldflags "$(LDFLAGS)" .)

$(DEV)/remote: FORCE
	$(call run,go run ./tools/remotepayload -out $@ -targets $(PAYLOADS))

$(CONPTY) &: tools/conpty/main.go
	$(call run,go run ./tools/conpty)

# Copied beside the dev executable when newer.
$(DEV)/%: resources/$(PLATFORM)/%
	$(call run,mkdir -p $(@D) && cp -p $< $@)
$(DEV)/%: resources/%
	$(call run,mkdir -p $(@D) && cp -p $< $@)

release: $(CONPTY)
	$(call run,go tool mygo build)
	$(total)

test:
	$(call run,go test ./...)
	$(total)

vet:
	$(call run,go vet ./...)
	$(total)

clean:
	$(call run,rm -rf $(DEV))
	$(total)
