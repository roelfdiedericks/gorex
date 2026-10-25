module gorex

go 1.27.1

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.6.3-0.20261021150203-80afe0af6f9d
	github.com/fsnotify/fsnotify v1.10.1
	github.com/hashicorp/yamux v0.1.2
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/stencil-hq/tern-sdk/go/tern v0.1.0
	github.com/yuin/goldmark v1.8.6
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo

// GoRex's MyGo: upstream main and the changes GoRex carries, branch
// gorex-upstream of the fork. To work on MyGo alongside, replace it with
// ../mygo in a go.work, which is not committed.
replace github.com/egoist/mygo => github.com/roelfdiedericks/mygo v0.0.0-20261024235949-fafeb6157ee3
