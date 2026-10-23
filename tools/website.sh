#!/bin/sh
# website.sh builds and publishes gorex.goclaw.org: the Makefile's
# website target runs "website.sh build", website-sync "website.sh sync",
# with the settings below as NAME=value arguments. The Makefile runs it
# in a subshell of its recipe's shell ("(set -- build ...; . website.sh)"),
# as on Windows a shell started from there gets no environment.
#
#   NAME, VERSION     the app's, from mygo.json
#   SITE_URL          https://gorex.goclaw.org
#   SITE_PLATFORMS    GOOS/GOARCH, comma separated: darwin ones build on
#                     MAC_HOST, the others here
#   SITE_BUILD        where the builds go, relative to the checkout
#   UPDATE_KEY        the secret key of mygo keygen, which signs updates
#   MAC_HOST, MAC_DIR the Mac that builds darwin, and its checkout, whose
#                     update key is in its mygo configuration directory
#   SSH               ssh, for the Mac
#   SITE_HOST, SITE_DIR, SITE_OWNER, RSYNC, RSYNC_SSH   where sync uploads
#
# build builds the platforms whose published version is older than
# VERSION, refusing one whose published version is newer, and stages the
# site in website/: updates/ (the signed archives, deltas, manifests,
# installers, Linux's install.sh) and download/ (each platform's installer
# under a name that stays). sync uploads it: updates/ and download/ only
# add and replace, the manifests last, so that apps never see a manifest
# whose archive is missing; the rest is mirrored, but for ISPConfig's
# error/, stats/ and robots.txt.
set -eu

die() {
	echo "website: $*" >&2
	exit 1
}

# newer succeeds when version $2 is newer than $1, number by number (awk:
# the sort on Windows' PATH may be its own, without -V).
newer() {
	awk -v a="$1" -v b="$2" 'BEGIN { split(a, x, "."); split(b, y, "."); for (i = 1; i <= 3; i++) { if (y[i] + 0 > x[i] + 0) exit 0; if (y[i] + 0 < x[i] + 0) exit 1 } exit 1 }'
}

build() {
	[ -f "$UPDATE_KEY" ] || die "no update key at $UPDATE_KEY (UPDATE_KEY)"
	# The Mac builds the commit from the fork, and a release is of a
	# commit anyway.
	git diff --quiet HEAD || die "the tree has changes: commit them first"
	git fetch -q fork
	head=$(git rev-parse HEAD)
	[ "$head" = "$(git rev-parse fork/master)" ] || die "HEAD is not fork/master: push it first"

	here="" mac=""
	mkdir -p "$(dirname "$SITE_BUILD")"
	for p in $(echo "$SITE_PLATFORMS" | tr , ' '); do
		t=$(echo "$p" | tr / -)
		manifest="$SITE_URL/updates/update-$t.json"
		code=$(curl -sS -o "$SITE_BUILD.json" -w '%{http_code}' "$manifest") || die "could not read $manifest"
		if [ "$code" = 200 ]; then
			v=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$SITE_BUILD.json")
			if [ "$v" = "$VERSION" ]; then
				echo "website: $p: $VERSION is published already"
				continue
			fi
			newer "$v" "$VERSION" || die "$p: $v is published; $VERSION must be newer"
		elif [ "$code" != 404 ]; then
			die "$manifest: HTTP $code"
		fi
		case $p in
		darwin/*) mac="$mac,$p" ;;
		*) here="$here,$p" ;;
		esac
	done
	rm -f "$SITE_BUILD.json"
	here=${here#,} mac=${mac#,}
	[ -n "$here$mac" ] || die "$VERSION is published for every platform: a release needs a new version"

	rm -rf "$SITE_BUILD"
	if [ -n "$here" ]; then
		echo "website: building $here"
		MYGO_UPDATER_PRIVATE_KEY=$(cat "$UPDATE_KEY") go tool mygo build -platform "$here" -o "$SITE_BUILD"
	fi
	if [ -n "$mac" ]; then
		echo "website: building $mac on $MAC_HOST"
		$SSH "$MAC_HOST" "set -e; cd $MAC_DIR; git fetch -q origin; git checkout -q -B master $head; rm -rf $SITE_BUILD;
			MYGO_UPDATER_PRIVATE_KEY=\"\$(cat \"\$HOME/Library/Application Support/mygo/update-keys/mygo-update.key\")\" go tool mygo build -platform $mac -o $SITE_BUILD"
		for p in $(echo "$mac" | tr , ' '); do
			t=$(echo "$p" | tr / -)
			mkdir -p "$SITE_BUILD/$t"
			$SSH "$MAC_HOST" "cd $MAC_DIR/$SITE_BUILD/$t && tar -cf - *.dmg *.tar.gz update-*.json \$(ls *.delta 2>/dev/null)" | tar -xf - -C "$SITE_BUILD/$t"
		done
	fi
	stage
}

# stage puts the builds in website/.
stage() {
	rm -rf website/download website/updates
	mkdir -p website/download website/updates
	cp resources/icon.png website/icon.png
	cp docs/screenshot.png website/screenshot.png
	for d in "$SITE_BUILD"/*/; do
		t=$(basename "$d")
		cp "$d"*.tar.gz "$d"update-*.json website/updates/
		for f in "$d"*.delta; do
			[ ! -e "$f" ] || cp "$f" website/updates/
		done
		case $t in
		windows-amd64)
			cp "$d$NAME Setup $VERSION amd64.exe" website/updates/
			cp "$d$NAME Setup $VERSION amd64.exe" "website/download/$NAME-Setup-amd64.exe"
			;;
		darwin-arm64)
			cp "$d$NAME $VERSION arm64.dmg" website/updates/
			cp "$d$NAME $VERSION arm64.dmg" "website/download/$NAME-arm64.dmg"
			;;
		linux-amd64)
			cp "${d}install.sh" website/updates/
			for f in "$d"*.deb; do
				cp "$f" website/updates/
				cp "$f" website/download/gorex-amd64.deb
			done
			;;
		esac
	done
	ls -l website/download website/updates
}

sync() {
	[ -n "$SITE_OWNER" ] || die "set SITE_OWNER to the ISPConfig site's user:group, e.g. web7:client1"
	ls website/updates/update-*.json >/dev/null 2>&1 || die "nothing built: make website first"
	up() {
		"$RSYNC" -rtpz --chown="$SITE_OWNER" --chmod=D755,F644 -e "$RSYNC_SSH" "$@"
	}
	up --exclude='update-*.json' website/updates/ "$SITE_HOST:${SITE_DIR}updates/"
	up website/download/ "$SITE_HOST:${SITE_DIR}download/"
	up website/updates/update-*.json "$SITE_HOST:${SITE_DIR}updates/"
	up --delete --filter='P /updates/' --filter='P /download/' --filter='P /error/' --filter='P /stats/' --filter='P /robots.txt' \
		--exclude=/updates/ --exclude=/download/ website/ "$SITE_HOST:$SITE_DIR"
	echo "website: published to $SITE_URL"
}

cmd=${1:-}
[ $# -gt 0 ] && shift
for a in "$@"; do
	case $a in
	[A-Z]*=*) export "$a" ;;
	*) die "not a setting: $a" ;;
	esac
done
case $cmd in
build) build ;;
sync) sync ;;
*) die "usage: website.sh build|sync [NAME=value]..." ;;
esac
