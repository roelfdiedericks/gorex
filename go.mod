module gorex

go 1.27.3

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/bvolpato/mermaid-go-renderer v0.13.0
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.6.3-0.20261021150203-80afe0af6f9d
	github.com/fsnotify/fsnotify v1.10.1
	github.com/hashicorp/yamux v0.1.2
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/stencil-hq/tern-sdk/go/tern v0.1.0
	github.com/yuin/goldmark v1.8.6
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc
	go.bug.st/serial v1.8.0
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dlclark/regexp2/v2 v2.11.0 // indirect
	github.com/dop251/goja v0.0.0-20261029234554-1329d889cf98 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

tool github.com/egoist/mygo/cmd/mygo

// GoRex's MyGo: upstream main and the changes GoRex carries, branch
// gorex-upstream-2 of the fork. To work on MyGo alongside, replace it with
// ../mygo in a go.work, which is not committed.
replace github.com/egoist/mygo => github.com/roelfdiedericks/mygo v0.0.0-20261112004408-bc1c409501ee
