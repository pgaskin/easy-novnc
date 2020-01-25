// +build novnc_generate

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	"github.com/shurcooL/vfsgen"
	"github.com/spkg/zipfs"
)

const noVNCZip = "https://github.com/novnc/noVNC/archive/master.zip"
const vncScript = `
	try {
		function parseQuery(e){for(var o=e.split("&"),n={},t=0;t<o.length;t++){var d=o[t].split("="),p=decodeURIComponent(d[0]),r=decodeURIComponent(d[1]);if(void 0===n[p])n[p]=decodeURIComponent(r);else if("string"==typeof n[p]){var i=[n[p],decodeURIComponent(r)];n[p]=i}else n[p].push(decodeURIComponent(r))}return n};
		fetch(parseQuery(window.location.search.replace(/^\?/, ""))["path"]).then(function(resp) {
			return resp.text();
		}).then(function (txt) {
			if (txt.indexOf("not websocket") == -1) alert(txt);
		});
	} catch (ex) {
		console.log(ex);
	}
`

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

	err = modifyZip(f.Name())
	if err != nil {
		panic(err)
	}

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

// modifyZip adds the custom easy-novnc code into the noVNC zip file.
func modifyZip(zf string) error {
	buf, err := ioutil.ReadFile(zf)
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return err
	}

	f, err := os.Create(zf)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	var found bool
	for _, e := range zr.File {
		var w io.Writer

		rc, err := e.Open()
		if err != nil {
			return err
		}

		fbuf, err := ioutil.ReadAll(rc)
		if err != nil {
			return err
		}

		if filepath.Base(e.Name) == "vnc.html" {
			found = true
			fbuf = bytes.ReplaceAll(fbuf, []byte("</body>"), []byte(fmt.Sprintf("<script>%s</script></body>", vncScript)))
			fi, err := os.Stat("novnc_generate.go")
			if err != nil {
				return err
			}
			w, err = zw.CreateHeader(&zip.FileHeader{
				Name:          e.Name,
				Flags:         e.Flags,
				Method:        e.Method,
				Modified:      fi.ModTime(),
				Extra:         e.Extra,
				ExternalAttrs: e.ExternalAttrs,
			})
		} else {
			w, err = zw.CreateHeader(&e.FileHeader)
		}

		if err != nil {
			return err
		}

		_, err = io.Copy(w, bytes.NewReader(fbuf))
		if err != nil {
			return err
		}
		rc.Close()
	}

	if !found {
		return errors.New("could not find vnc.html")
	}

	return nil
}
