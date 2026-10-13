module gorex

go 1.27.1

require (
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.4.8
	github.com/fsnotify/fsnotify v1.10.1
	github.com/hashicorp/yamux v0.1.2
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/alecthomas/chroma/v2 v2.27.0 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/yuin/goldmark v1.8.6 // indirect
	github.com/yuin/goldmark-highlighting/v2 v2.0.0-20230729083705-37449abec8cc // indirect
	golang.org/x/net v0.26.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo

replace github.com/egoist/mygo => ../mygo
