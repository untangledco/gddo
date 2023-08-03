// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package server

import (
	"fmt"
	htemp "html/template"
	"io"
	"io/fs"
	"net/http"
	ttemp "text/template"

	"github.com/dustin/go-humanize"

	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/static"
)

type TemplateMap map[string]interface {
	Execute(io.Writer, interface{}) error
}

func (m TemplateMap) ExecuteHTML(resp http.ResponseWriter, name string, data interface{}) error {
	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	return m.Execute(resp, name, data)
}

func (m TemplateMap) Execute(w io.Writer, name string, data interface{}) error {
	t := m[name]
	if t == nil {
		return fmt.Errorf("template %s not found", name)
	}
	return t.Execute(w, data)
}

func (m TemplateMap) HTML(name string) *htemp.Template {
	return m[name].(*htemp.Template)
}

func (m TemplateMap) Text(name string) *ttemp.Template {
	return m[name].(*ttemp.Template)
}

func (m TemplateMap) ParseHTML(name string, funcs htemp.FuncMap, fsys fs.FS) error {
	r := (*Renderer)(nil)
	t := htemp.New("layout.html").
		Funcs(funcs).
		Funcs(r.HTMLFuncs()).
		Funcs(htemp.FuncMap{
			"templateName": func() string { return name },
		})
	if _, err := t.ParseFS(fsys, "layout.html", name); err != nil {
		return err
	}
	m[name] = t
	return nil
}

func (m TemplateMap) ParseText(name string, funcs ttemp.FuncMap, fsys fs.FS) error {
	r := (*Renderer)(nil)
	t := ttemp.New(name).Funcs(funcs).Funcs(r.GeminiFuncs())
	if _, err := t.ParseFS(fsys, name); err != nil {
		return err
	}
	m[name] = t
	return nil
}

func (s *Server) parseHTMLTemplates(m TemplateMap, files *httputil.FileServer) error {
	fsys, err := fs.Sub(static.FS, "templates")
	if err != nil {
		return err
	}

	tmpls := []string{
		"about.html",
		"doc.html",
		"index.html",
		"versions.html",
		"platforms.html",
		"imports.html",
		"notfound.html",
		"search.html",
		"tools.html",
	}
	funcs := htemp.FuncMap{
		"static_path": func(name string) string {
			return "/-/" + name + files.QueryParam(name)
		},
		"humanize": humanize.Time,
		"config":   func() *Config { return s.cfg },
	}
	for _, tmpl := range tmpls {
		err := m.ParseHTML(tmpl, funcs, fsys)
		if err != nil {
			return err
		}
	}
	tfuncs := ttemp.FuncMap{
		"config": func() *Config { return s.cfg },
	}
	err = m.ParseText("opensearch.xml", tfuncs, fsys)
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) parseGeminiTemplates(m TemplateMap) error {
	fsys, err := fs.Sub(static.FS, "templates")
	if err != nil {
		return err
	}

	tmpls := []string{
		"index.gmi",
		"about.gmi",
		"search.gmi",
		"doc.gmi",
		"versions.gmi",
		"platforms.gmi",
		"imports.gmi",
	}
	funcs := ttemp.FuncMap{
		"humanize": humanize.Time,
		"config":   func() *Config { return s.cfg },
	}
	for _, tmpl := range tmpls {
		err := m.ParseText(tmpl, funcs, fsys)
		if err != nil {
			return err
		}
	}
	return nil
}
