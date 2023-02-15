# iGo+ The Go/Go+ Interpreter

[![Go1.14](https://github.com/goplus/igop/workflows/Go1.14/badge.svg)](https://github.com/goplus/igop/actions/workflows/go114.yml)
[![Go1.15](https://github.com/goplus/igop/workflows/Go1.15/badge.svg)](https://github.com/goplus/igop/actions/workflows/go115.yml)
[![Go1.16](https://github.com/goplus/igop/workflows/Go1.16/badge.svg)](https://github.com/goplus/igop/actions/workflows/go116.yml)
[![Go1.17](https://github.com/goplus/igop/workflows/Go1.17/badge.svg)](https://github.com/goplus/igop/actions/workflows/go117.yml)
[![Go1.18](https://github.com/goplus/igop/workflows/Go1.18/badge.svg)](https://github.com/goplus/igop/actions/workflows/go118.yml)
[![Go1.19](https://github.com/goplus/igop/workflows/Go1.19/badge.svg)](https://github.com/goplus/igop/actions/workflows/go119.yml)
[![Go1.20](https://github.com/goplus/igop/workflows/Go1.20/badge.svg)](https://github.com/goplus/igop/actions/workflows/go120.yml)


### Go Version

- Go1.14 ~ Go1.20
- macOS Linux Windows  WebAssembly GopherJS and more.

### ABI

support ABI0 and ABIInternal

- ABI0 stack-based ABI
- ABIInternal [register-based Go calling convention proposal](https://golang.org/design/40724-register-calling)

    - Go1.17: amd64
    - Go1.18: amd64 arm64 ppc64/ppc64le
    - Go1.19: amd64 arm64 ppc64/ppc64le riscv64
    - Go1.20: amd64 arm64 ppc64/ppc64le riscv64

### Generics

- support generics (Go1.18/Go1.19/Go1.20)
- support [Go1.20 nested type-parameterized declarations](https://github.com/golang/go/blob/master/test/typeparam/nested.go) on Go1.18/Go1.19 (Experimental)

### install igop command line

Go version < 1.17:

```shell
go get -u github.com/goplus/igop/cmd/igop
```

Go version >= 1.17:

```shell
go install github.com/goplus/igop/cmd/igop@latest
```

### igop command

```
igop             # igop repl mode
igop run         # run a Go/Go+ package
igop build       # compile a Go/Go+ package
igop test        # test a package
igop verson      # print version
igop export      # export Go package to igop builtin package
```

### igop repl mode

```shell
igop                       # run repl mode, support Go/Go+
igop repl                  # run repl mode, support Go/Go+
igop repl -gop=false       # run repl mode, disable Go+ syntax
```

### igop test unsupport features

- test -fuzz
- test -cover

### igop demo

#### go js playground (gopherjs)

- <https://jsplay.goplus.org/>
- <https://github.com/goplusjs/play>

#### go repl playground (gopherjs/wasm)

- <https://repl.goplus.org/>
- <https://github.com/goplusjs/repl>

#### ispx

<https://github.com/goplus/ispx>

#### run simple Go source demo

```go
package main

import (
	"github.com/goplus/igop"
	_ "github.com/goplus/igop/pkg/fmt"
)

var source = `
package main
import "fmt"
type T struct {}
func (t T) String() string {
	return "Hello, World"
}
func main() {
	fmt.Println(&T{})
}
`

func main() {
	_, err := igop.RunFile("main.go", source, nil, 0)
	if err != nil {
		panic(err)
	}
}
```

#### run simple Go+ source demo

```go
package main

import (
	"github.com/goplus/igop"
	_ "github.com/goplus/igop/gopbuild"
)

var gopSrc string = `
fields := [
	"engineering",
	"STEM education", 
	"and data science",
]

println "The Go+ language for", fields.join(", ")
`

func main() {
	_, err := igop.RunFile("main.gop", gopSrc, nil, 0)
	if err != nil {
		panic(err)
	}
}
```

#### run hugo demo

```go

// igop_hugo/main.go
package main

import (
	"os"
	"path/filepath"

	"github.com/goplus/igop"
	_ "github.com/goplus/igop/pkg"
	_ "github.com/goplus/ipkg/golang.org/x/image/vector"
	_ "github.com/goplus/ipkg/golang.org/x/sys/unix"
	_ "github.com/goplus/reflectx/icall/icall65536"
)

func main() {
	root, args := os.Args[1], os.Args[2:]
	if !filepath.IsAbs(root) {
		wd, _ := os.Getwd()
		root = filepath.Join(wd, root)
	}
	code, err := igop.Run(root, args, igop.EnableDumpImports)
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}
```

```shell
git clone https://github.com/gohugoio/hugo
cd hugo
go mod tidy
cd ..
igop_hugo ./hugo new site quickstart
cd quickstart
git init
git submodule add https://github.com/theNewDynamic/gohugo-theme-ananke themes/ananke
echo "theme = 'ananke'" >> config.toml
igop_hugo ../hugo server
```
