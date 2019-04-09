// +build novnc_generate

package main

import (
	"io"
	"io/ioutil"
	"net/http"

	"github.com/shurcooL/vfsgen"
	"github.com/spkg/zipfs"
)

const noVNCZip = "https://github.com/novnc/noVNC/archive/master.zip"

func main() {
	resp, err := http.Get(noVNCZip)
	if err != nil {
		panic(err)
	}

	f, err := ioutil.TempFile("", "novnc*.zip")
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		panic(err)
	}

	f.Close()
	resp.Body.Close()

	zfs, err := zipfs.New(f.Name())
	if err != nil {
		panic(err)
	}

	err = vfsgen.Generate(zfs, vfsgen.Options{
		Filename:        "novnc_generated.go",
		PackageName:     "main",
		VariableName:    "noVNC",
		VariableComment: "noVNC is the latest version of noVNC from GitHub as a http.FileSystem",
	})
	if err != nil {
		panic(err)
	}
}
