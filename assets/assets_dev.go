// +build dev

package assets

import (
	"net/http"
	"os"
	"path"
)

type filteredDir string
type filteredFile struct {
	http.File
}

func shouldFilter(base string) bool {
	switch {
	case base == ".": return false
	case base[0] == '.': return true
	case path.Ext(base) == ".go": return true
	default: return false
	}
}

func (d filteredDir) Open(name string) (http.File, error) {
	name = path.Clean(name)
	if shouldFilter(path.Base(name)) {
		return nil, os.ErrNotExist
	}
	res, err := http.Dir(d).Open(name)
	return filteredFile{res}, err
}

func (f filteredFile) Readdir(count int) ([]os.FileInfo, error) {
	fis, err := f.File.Readdir(count)
	for i, n := 0, len(fis); i < n; {
		if !shouldFilter(fis[i].Name()) { i++; continue }
		n--
		if i != n { fis[i] = fis[n] }
		fis = fis[:n]
		// Do not increment i.
	}
	return fis, err
}

// Assumes the current directory is kq-live.
var FS http.FileSystem = filteredDir("assets")

// Resets the root directory. Used by the generator, which knows its working
// directory will be the one containing it.
func ResetRoot(root string) {
	FS = filteredDir(root)
}
