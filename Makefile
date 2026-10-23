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

# The version CHANGELOG.md is at: its first "## X.Y.Z" heading.
CL_VERSION := $(shell sed -n 's/^## \([0-9][^ ]*\).*/\1/p' CHANGELOG.md | head -n 1)

# NEWER succeeds when VERSION is newer than $v, number by number (awk, as
# sort on Windows' PATH may be its own, without -V).
NEWER = awk -v a="$$v" -v b="$(VERSION)" 'BEGIN { split(a, x, "."); split(b, y, "."); for (i = 1; i <= 3; i++) { if (y[i] + 0 > x[i] + 0) exit 0; if (y[i] + 0 < x[i] + 0) exit 1 } exit 1 }'

# The site, gorex.goclaw.org: the platforms it carries, where it is built,
# and where it is published. SITE_OWNER is the ISPConfig site's user and
# group. UPDATE_KEY is the secret key of mygo keygen, which signs updates.
SITE_PLATFORMS ?= windows/amd64
SITE_URL       := https://gorex.goclaw.org
SITE_BUILD     := build/site
SITE_HOST      ?= root@gorex.goclaw.org
SITE_DIR       ?= /var/www/gorex.goclaw.org/web/
SITE_OWNER     ?= web7:client1
UPDATE_KEY     ?= $(subst \,/,$(APPDATA))/mygo/update-keys/mygo-update.key
RSYNC          ?= rsync
ifeq ($(GOOS),windows)
# MSYS rsync cannot talk through Windows OpenSSH: it uses MSYS ssh with the
# Windows user's key and known hosts.
WIN_SSH_DIR := $(subst \,/,$(USERPROFILE))/.ssh
SITE_KEY    ?= $(WIN_SSH_DIR)/id_dsa
RSYNC_SSH   ?= C:/msys64/usr/bin/ssh.exe -o BatchMode=yes -o ConnectTimeout=10 -i $(SITE_KEY) -o UserKnownHostsFile=$(WIN_SSH_DIR)/known_hosts
else
RSYNC_SSH   ?= ssh -o BatchMode=yes -o ConnectTimeout=10
endif
RSYNC_UP = "$(RSYNC)" -rtpz --chown=$(SITE_OWNER) --chmod=D755,F644 -e "$(RSYNC_SSH)"

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

.PHONY: dev run release version website website-sync publish test vet clean FORCE

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

release: version $(CONPTY)
	$(call run,go tool mygo build)
	$(total)

# The version is the top "## X.Y.Z" of CHANGELOG.md, which mygo.json's
# version must match: mygo build takes it from there, and the changelog's
# section of it is the release notes updates show.
version:
	@[ -n "$(CL_VERSION)" ] || { echo "[make] version: CHANGELOG.md has no ## X.Y.Z heading"; exit 1; }
	@[ "$(CL_VERSION)" = "$(VERSION)" ] || { echo "[make] version: CHANGELOG.md is at $(CL_VERSION), mygo.json at $(VERSION): make them the same"; exit 1; }

# website builds what gorex.goclaw.org serves into website/: the installer
# to download (download/, with VERSION) and the updates installed apps
# fetch (updates/: the signed archive, deltas from the versions published,
# and the manifest of each platform). It refuses a version that is not
# newer than the one published.
website: version $(CONPTY)
	@[ -f "$(UPDATE_KEY)" ] || { echo "[make] website: no update key at $(UPDATE_KEY) (UPDATE_KEY)"; exit 1; }
	$(call run,mkdir -p build && for p in $(subst $(comma), ,$(SITE_PLATFORMS)); do \
		m=update-$$(echo $$p | tr / -).json; \
		code=$$(curl -sS -o $(SITE_BUILD).$$m -w '%{http_code}' $(SITE_URL)/updates/$$m) || exit 1; \
		if [ "$$code" = 200 ]; then \
			v=$$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' $(SITE_BUILD).$$m); \
			$(NEWER) || { echo "$$p: $$v is published; $(VERSION) must be newer"; exit 1; }; \
		elif [ "$$code" != 404 ]; then \
			echo "$(SITE_URL)/updates/$$m: HTTP $$code"; exit 1; \
		fi; rm -f $(SITE_BUILD).$$m; \
	done)
	$(call run,rm -rf $(SITE_BUILD) && MYGO_UPDATER_PRIVATE_KEY="$$(cat "$(UPDATE_KEY)")" go tool mygo build -platform $(SITE_PLATFORMS) -o $(SITE_BUILD))
	$(call run,rm -rf website/download website/updates && mkdir -p website/download website/updates && \
		cp resources/icon.png website/icon.png && cp docs/screenshot.png website/screenshot.png && \
		for d in $(SITE_BUILD)/*/; do \
			cp "$$d"*.tar.gz "$$d"update-*.json website/updates/ && \
			for f in "$$d"*.delta; do [ ! -e "$$f" ] || cp "$$f" website/updates/; done; \
		done && \
		if [ -f "$(SITE_BUILD)/windows-amd64/$(NAME) Setup $(VERSION) amd64.exe" ]; then \
			cp "$(SITE_BUILD)/windows-amd64/$(NAME) Setup $(VERSION) amd64.exe" website/updates/ && \
			cp "$(SITE_BUILD)/windows-amd64/$(NAME) Setup $(VERSION) amd64.exe" website/download/$(NAME)-Setup-amd64.exe; \
		fi && \
		echo $(VERSION) > website/download/VERSION && ls -l website/download website/updates)
	$(total)

# website-sync publishes what website built to gorex.goclaw.org. updates/
# keeps every version, which deltas are made from, and its manifests go
# last, so that apps never see a manifest whose archive is missing. The
# rest is mirrored; ISPConfig's error/, stats/ and robots.txt stay.
# Ownership is the site's, which Apache reads the files through. A sync
# cut short is run again as it is; publish builds and syncs.
website-sync:
	@[ -n "$(SITE_OWNER)" ] || { echo "[make] $@: set SITE_OWNER to the ISPConfig site's user:group, e.g. web7:client1"; exit 1; }
	@[ -f website/download/VERSION ] || { echo "[make] $@: nothing built: make website first"; exit 1; }
	$(call run,$(RSYNC_UP) --exclude='update-*.json' website/updates/ "$(SITE_HOST):$(SITE_DIR)updates/")
	$(call run,$(RSYNC_UP) website/updates/update-*.json "$(SITE_HOST):$(SITE_DIR)updates/")
	$(call run,$(RSYNC_UP) --delete --filter='P /updates/' --filter='P /error/' --filter='P /stats/' --filter='P /robots.txt' \
		--exclude=/updates/ website/ "$(SITE_HOST):$(SITE_DIR)")
	$(total)

publish: website
	@$(MAKE) --no-print-directory website-sync

test:
	$(call run,go test ./...)
	$(total)

vet:
	$(call run,go vet ./...)
	$(total)

clean:
	$(call run,rm -rf $(DEV))
	$(total)
