package assets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	urllib "net/url"
	pathlib "path"
	"strings"
)

func assetUri(fs http.FileSystem, path string) (url template.URL, err error) {
	// Find the asset, relative to the template file. Produce a URI that will
	// serve it, either as a data: URI or via web server.
	var f http.File
	if len(path) > 0 && path[0] == '/' {
		f, err = FS.Open("/static" + path)
	} else {
		f, err = fs.Open(path)
	}
	if err != nil {
		return
	}
	defer f.Close()
	// TODO: For large assets, or those with unknown extensions, register them
	// with a web server and return a URL to that server.
	// Assume the file extension matches the contents. Whether or not to use
	// base64 encoding is guessed based on the type.
	useBase64, mime := false, ""
	switch pathlib.Ext(path) {
	case ".css":
		useBase64, mime = false, "text/css"
	case ".gif":
		useBase64, mime = true, "image/gif"
	case ".ico":
		useBase64, mime = true, "image/x-icon"
	case ".jpg", ".jpeg":
		useBase64, mime = true, "image/jpeg"
	case ".json":
		useBase64, mime = false, "application/json"
	case ".png":
		useBase64, mime = true, "image/png"
	case ".svg":
		useBase64, mime = false, "image/svg+xml"
	default:
		useBase64, mime = true, "application/octet-stream"
	}

	var builder strings.Builder
	if _, err = builder.WriteString("data:"); err != nil {
		return
	}
	if _, err = builder.WriteString(mime); err != nil {
		return
	}
	if useBase64 {
		if _, err = builder.WriteString(";base64,"); err != nil {
			return
		}
		enc := base64.NewEncoder(base64.StdEncoding, &builder)
		if _, err = io.Copy(enc, f); err != nil {
			return
		}
		if err = enc.Close(); err != nil {
			return
		}
	} else {
		if _, err = builder.WriteString(";charset=UTF-8,"); err != nil {
			return
		}
		var b strings.Builder
		if _, err = io.Copy(&b, f); err != nil {
			return
		}
		if _, err = builder.WriteString(urllib.PathEscape(b.String())); err != nil {
			return
		}
	}
	url = template.URL(builder.String())
	return
}

func parseJson(in interface{}) (v interface{}, err error) {
	var data []byte
	switch in := in.(type) {
	case []byte:
		data = in
	case string:
		data = []byte(in)
	default:
		err = fmt.Errorf("parseJson: expected []byte or string; got %T", in)
		return
	}
	err = json.Unmarshal(data, &v)
	return
}

// Reads from the file and parses the result into the given template.
func ParseTemplateFile(t *template.Template, f http.File) (*template.Template, error) {
	var contents strings.Builder
	if _, err := io.Copy(&contents, f); err != nil {
		return nil, err
	}
	return t.Parse(contents.String())
}

// Reads from the file and parses the result into a template. The following
// extra functions are available to the template:
//
// - assetUri(path string) URL: Given a path string, returns a URI that can be
//   used to load the asset. If the path is relative, it is accessed in the
//   assetFS file system. Otherwise it is based from /static/ in assets.FS.
// - parseJson(data interface{}) interface{}: Given a string or byte array,
//   parses it as JSON and returns an object representing the result.
func LoadTemplateFile(f http.File, assetFS http.FileSystem) (*template.Template, error) {
	funcs := template.FuncMap{
		"assetUri": func(path string) (template.URL, error) {
			return assetUri(assetFS, path)
		},
		"parseJson": parseJson,
	}
	var name string
	if namer, ok := f.(interface{ Name() string }); ok {
		name = namer.Name()
	} else {
		fi, err := f.Stat()
		if err != nil {
			return nil, err
		}
		name = fi.Name()
	}
	return ParseTemplateFile(template.New(name).Funcs(funcs), f)
}

// Reads the given asset from `assets.FS` and parses the result into the
// given template.
func ParseTemplate(t *template.Template, assetPath string) (*template.Template, error) {
	if f, err := FS.Open(assetPath); err != nil {
		return nil, err
	} else {
		return ParseTemplateFile(t, f)
	}
}

// Reads the given asset from `assets.FS` and parses the result into a
// template. The following extra functions are available to the template:
//
// - assetUri(path string) URL: Given a path string, returns a URI that can be
//   used to load the asset. If the path is relative, it is accessed in the
//   assetFS file system. Otherwise it is based from /static/ in assets.FS.
// - parseJson(data interface{}) interface{}: Given a string or byte array,
//   parses it as JSON and returns an object representing the result.
func LoadTemplate(assetPath string, assetFS http.FileSystem) (*template.Template, error) {
	if f, err := FS.Open(assetPath); err != nil {
		return nil, err
	} else {
		return LoadTemplateFile(f, assetFS)
	}
}
