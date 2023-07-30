// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package httputil

import (
	"crypto/sha1"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"time"
)

// FileServer serves static files.
type FileServer struct {
	fsys   fs.FS
	hashes map[string]string
}

// NewFileServer returns a new FileServer which serves files from the given
// filesystem.
func NewFileServer(fsys fs.FS) *FileServer {
	return &FileServer{
		fsys:   fsys,
		hashes: make(map[string]string),
	}
}

// FileHandler returns a handler that serves a single file. The file is
// specified by a slash separated path relative to the static server's
// filesystem.
func (s *FileServer) FileHandler(filename string) http.Handler {
	h, err := s.newFileHandler(filename)
	if err != nil {
		panic(err)
	}
	return h
}

// QueryParam returns the hash for the given filename as a query parameter.
func (s *FileServer) QueryParam(filename string) string {
	hash := s.hashes[filename]
	if hash == "" {
		return ""
	}
	return "?v=" + hash
}

func (s *FileServer) newFileHandler(filename string) (*fileHandler, error) {
	f, err := s.fsys.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	w := sha1.New()
	if _, err := io.Copy(w, f); err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", w.Sum(nil))
	s.hashes[filename] = hash

	return &fileHandler{
		fsys: s.fsys,
		name: filename,
		hash: hash,
	}, nil
}

type fileHandler struct {
	fsys fs.FS
	name string
	hash string
}

func (h *fileHandler) error(w http.ResponseWriter, r *http.Request, status int, err error) {
	http.Error(w, http.StatusText(status), status)
}

func (h *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := path.Clean(r.URL.Path)
	if p != r.URL.Path {
		http.Redirect(w, r, p, 301)
		return
	}

	f, err := h.fsys.Open(h.name)
	if err != nil {
		h.error(w, r, http.StatusNotFound, err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		h.error(w, r, http.StatusNotFound, err)
		return
	}

	maxAge := 24 * time.Hour
	if r.FormValue("v") != "" {
		maxAge = 365 * 24 * time.Hour
	}

	cacheControl := fmt.Sprintf("public, max-age=%d", maxAge/time.Second)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Etag", fmt.Sprintf(`"%s"`, h.hash))

	http.ServeContent(w, r, h.name, fi.ModTime(), f.(io.ReadSeeker))
}
