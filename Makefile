# GoRex's builds.
#
#   make dev           GoRex for this machine's platform in dev/, with the
#                      servers it copies to ssh hosts in dev/remote, one per
#                      platform in PAYLOADS; go build decides what changed
#   make run           dev, and run it
#   make release       go tool mygo build: every server, the installer
#   make website       the site's installer and signed updates, in website/
#   make website-sync  what website built, published to gorex.goclaw.org
#   make publish       website, then website-sync
#   make icon          resources/icon.png, redrawn from tools/mkicon/icon.svg
#   make test, vet, clean
#
# A release's version is the top "## X.Y.Z" of CHANGELOG.md, which
# mygo.json's version must match.

# The app and the servers build side by side.
MAKEFLAGS += -j

DEV      := dev
PAYLOADS ?= linux/amd64,linux/arm64,darwin/amd64,darwin/arm64

# This machine's platform, as Go names it, e.g. windows-amd64.
GOENV    := $(shell go env GOOS GOARCH)
GOOS     := $(word 1,$(GOENV))
GOARCH   := $(word 2,$(GOENV))
PLATFORM := $(GOOS)-$(GOARCH)

# On Windows the icon is in the executable: a resource object of it that
# go build links in (tools/devicon), as mygo build does for its builds.
ICON_SYSO := $(if $(filter windows,$(GOOS)),mygo_windows_$(GOARCH).syso)

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

# gorex.com, GoRex's console program on Windows (cmd/gorex), which a shell
# runs for `gorex` before GoRex.exe: beside GoRex.exe in dev, and from
# resources in the installed app's bin, which the installer puts on the
# PATH (packaging/installer.nsh).
CLI_COM := $(if $(filter windows,$(GOOS)),resources/windows-$(GOARCH)/bin/gorex.com)
DEV_COM := $(if $(filter windows,$(GOOS)),$(DEV)/gorex.com)

# The version CHANGELOG.md is at: its first "## X.Y.Z" heading.
CL_VERSION := $(shell sed -n 's/^## \([0-9][^ ]*\).*/\1/p' CHANGELOG.md | head -n 1)

# The site, gorex.goclaw.org: the platforms it carries, where they are
# built (darwin and linux on MAC_HOST, in its checkout MAC_DIR), and where it is
# published. SITE_OWNER is the ISPConfig site's user and group.
# UPDATE_KEY is the secret key of mygo keygen, which signs updates; the
# Mac has its own copy, in its mygo configuration directory.
SITE_PLATFORMS ?= windows/amd64,darwin/arm64,linux/amd64
SITE_URL       := https://gorex.goclaw.org
SITE_BUILD     := build/site
SITE_HOST      ?= root@gorex.goclaw.org
SITE_DIR       ?= /var/www/gorex.goclaw.org/web/
SITE_OWNER     ?= web7:client1
UPDATE_KEY     ?= $(subst \,/,$(APPDATA))/mygo/update-keys/mygo-update.key
MAC_HOST       ?= rodent@192.168.0.224
MAC_DIR        ?= dev/gorex-build/gorex
RSYNC          ?= rsync
ifeq ($(GOOS),windows)
SSH         ?= C:/Windows/System32/OpenSSH/ssh.exe -o BatchMode=yes -o ConnectTimeout=10
# MSYS rsync cannot talk through Windows OpenSSH: it uses MSYS ssh with the
# Windows user's key and known hosts.
WIN_SSH_DIR := $(subst \,/,$(USERPROFILE))/.ssh
SITE_KEY    ?= $(WIN_SSH_DIR)/id_dsa
RSYNC_SSH   ?= C:/msys64/usr/bin/ssh.exe -o BatchMode=yes -o ConnectTimeout=10 -i $(SITE_KEY) -o UserKnownHostsFile=$(WIN_SSH_DIR)/known_hosts
else
SSH         ?= ssh -o BatchMode=yes -o ConnectTimeout=10
RSYNC_SSH   ?= ssh -o BatchMode=yes -o ConnectTimeout=10
endif
# tools/website.sh takes them as NAME=value arguments, and runs in a
# subshell of the recipe's shell: in make's Windows port, a shell that
# shell starts gets none of the environment, which go needs.
SITE_VARS := NAME VERSION SITE_URL SITE_PLATFORMS SITE_BUILD UPDATE_KEY MAC_HOST MAC_DIR SSH \
	SITE_HOST SITE_DIR SITE_OWNER RSYNC RSYNC_SSH
SITE_ARGS = $(foreach v,$(SITE_VARS),'$(v)=$($(v))')

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

.PHONY: dev run release version website website-sync publish icon test vet clean FORCE

dev: $(EXE) $(DEV_COM) $(DEV)/remote $(PLAT_RES) $(TOP_RES)
	$(total)

run: dev
	$(call run,./$(EXE))

$(EXE): FORCE $(ICON_SYSO)
	$(call run,go build -o $@ -ldflags "$(LDFLAGS)" .)

$(ICON_SYSO): resources/icon.png tools/devicon/main.go
	$(call run,go run ./tools/devicon -out $@ -arch $(GOARCH))

$(DEV_COM) $(CLI_COM): FORCE
	$(call run,go build -o $@ -ldflags "-X 'main.version=$(VERSION)'" ./cmd/gorex)

$(DEV)/remote: FORCE
	$(call run,go run ./tools/remotepayload -out $@ -targets $(PAYLOADS))

$(CONPTY) &: tools/conpty/main.go
	$(call run,go run ./tools/conpty)

# Copied beside the dev executable when newer.
$(DEV)/%: resources/$(PLATFORM)/%
	$(call run,mkdir -p $(@D) && cp -p $< $@)
$(DEV)/%: resources/%
	$(call run,mkdir -p $(@D) && cp -p $< $@)

release: version $(CONPTY) $(CLI_COM)
	$(call run,go tool mygo build)
	$(total)

# The version is the top "## X.Y.Z" of CHANGELOG.md, which mygo.json's
# version must match: mygo build takes it from there, and the changelog's
# section of it is the release notes updates show.
version:
	@[ -n "$(CL_VERSION)" ] || { echo "[make] version: CHANGELOG.md has no ## X.Y.Z heading"; exit 1; }
	@[ "$(CL_VERSION)" = "$(VERSION)" ] || { echo "[make] version: CHANGELOG.md is at $(CL_VERSION), mygo.json at $(VERSION): make them the same"; exit 1; }

# website builds what gorex.goclaw.org serves into website/, for each
# platform whose published version is older (darwin and linux on the Mac); see
# tools/website.sh. website-sync publishes it; publish does both.
website: version $(CONPTY) $(CLI_COM)
	$(call run,(set -- build $(SITE_ARGS); . ./tools/website.sh))
	$(total)

website-sync:
	$(call run,(set -- sync $(SITE_ARGS); . ./tools/website.sh))
	$(total)

publish: website
	@$(MAKE) --no-print-directory website-sync

test:
	$(call run,go test ./...)
	$(total)

vet:
	$(call run,go vet ./...)
	$(total)

# icon redraws the app's icon, from which mygo build makes every
# platform's, from its master, tools/mkicon/icon.svg.
icon: resources/icon.png
	$(total)

resources/icon.png: tools/mkicon/icon.svg tools/mkicon/main.go
	$(call run,go run ./tools/mkicon)

clean:
	$(call run,rm -rf $(DEV))
	$(total)
