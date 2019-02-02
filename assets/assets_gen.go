// +build ignore

package main

import (
	"github.com/ughoavgfhw/kq-live/assets"
	"github.com/shurcooL/vfsgen"
)

func main() {
	// `go generate` runs with this directory as the current directory.
	assets.ResetRoot(".")
	err := vfsgen.Generate(assets.FS, vfsgen.Options{
		Filename: "assets.gen.go",
		PackageName: "assets",
		BuildTags: "!dev",
		VariableName: "FS",
	})
	if err != nil {
		panic(err)
	}
}
