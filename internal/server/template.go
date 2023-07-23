// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package server

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	goprinter "go/printer"
	"go/token"
	htemp "html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	ttemp "text/template"

	"github.com/dustin/go-humanize"

	"git.sr.ht/~sircmpwn/gddo/internal/gemini"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/internal/printer"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

func (p *Package) IsCommand() bool {
	return p.Name == "main"
}

func (p *Package) PageName() string {
	if p.ImportPath == stdlib.ModulePath {
		return "Standard library"
	}
	if p.Name != "" && p.Name != "main" {
		return p.Name
	}
	return path.Base(p.ImportPath)
}

func (p *Package) Cgo() bool {
	for i := range p.Imports {
		if p.Imports[i] == "C" {
			return true
		}
	}
	return false
}

func (p *Package) View(view string) string {
	var b strings.Builder
	b.WriteByte('?')
	amp := false
	if len(view) != 0 {
		b.WriteString("view=")
		b.WriteString(view)
		amp = true
	}
	if p.platformParam {
		if amp {
			b.WriteByte('&')
		}
		b.WriteString("platform=")
		b.WriteString(url.QueryEscape(p.Platform))
	}
	return b.String()
}

func (p *Package) PlatformParam() string {
	if !p.platformParam {
		return ""
	}
	var b strings.Builder
	b.WriteString("?platform=")
	b.WriteString(url.QueryEscape(p.Platform))
	return b.String()
}

func (p *Package) VersionParam() string {
	if p.Version == p.LatestVersion {
		return ""
	}
	return "@" + p.Version
}

func (p *Package) SourceLink(pos token.Pos, text string, textOnlyOK bool) htemp.HTML {
	position := p.fset.Position(pos)
	if p.Reference == "" || position.Line == 0 || p.Project == nil {
		if textOnlyOK {
			return htemp.HTML(htemp.HTMLEscapeString(text))
		}
		return ""
	}
	link := p.Project.Line(p.Reference, p.Dir, position.Filename,
		strconv.Itoa(position.Line))
	return htemp.HTML(fmt.Sprintf(`<a title="View Source" rel="noopener nofollow" href="%s">%s</a>`,
		htemp.HTMLEscapeString(link),
		htemp.HTMLEscapeString(text)))
}

// HTML returns formatted HTML for the doc comment text.
func (p *Package) HTML(text string) htemp.HTML {
	return htemp.HTML(p.Doc.HTML(text))
}

// Gemini returns formatted Gemini content for the doc comment text.
func (p *Package) Gemini(text string) string {
	return string(gemini.Print(p.Doc.Parser().Parse(text)))
}

// Function formats a function declaration into a single line.
func (p *Package) Function(decl *ast.FuncDecl) string {
	var out strings.Builder
	config := goprinter.Config{
		Mode:     goprinter.UseSpaces,
		Tabwidth: 4,
	}
	config.Fprint(&out, p.fset, decl)
	return out.String()
}

type Example struct {
	*doc.Example
	ID     string
	Symbol string
	Suffix string
	Play   bool
	obj    interface{}
	pkg    *Package
}

func (e *Example) PlayID() string {
	return e.Symbol + "-" + e.Example.Suffix
}

func (e *Example) HTML(text string) htemp.HTML {
	return e.pkg.HTML(text)
}

func (e *Example) Gemini(text string) string {
	return e.pkg.Gemini(text)
}

func (e *Example) Code() htemp.HTML {
	c := printer.PrintExample(e.pkg.fset, e.Example)
	return e.pkg.code(c, nil)
}

func (e *Example) GeminiCode() string {
	return e.pkg.GeminiCode(e.Example.Code)
}

func (p *Package) addExamples(obj interface{}, symbol string, examples []*doc.Example) {
	for _, example := range examples {
		suffix := strings.Title(example.Suffix)
		ex := &Example{
			Example: example,
			ID:      exampleID(symbol, suffix),
			Symbol:  symbol,
			Suffix:  suffix,
			obj:     obj,
			pkg:     p,
			// Only show play links for packages within the standard library.
			// TODO: Always show play links
			Play: example.Play != nil && stdlib.Contains(p.ImportPath),
		}
		p.examples = append(p.examples, ex)
	}
}

func exampleID(symbol, suffix string) string {
	switch {
	case symbol == "" && suffix == "":
		return "example-package"
	case symbol == "" && suffix != "":
		return "example-package-" + suffix
	case symbol != "" && suffix == "":
		return "example-" + symbol
	case symbol != "" && suffix != "":
		return "example-" + symbol + "-" + suffix
	default:
		panic("unreachable")
	}
}

type byExampleSymbol []*Example

func (e byExampleSymbol) Len() int           { return len(e) }
func (e byExampleSymbol) Less(i, j int) bool { return e[i].Symbol < e[j].Symbol }
func (e byExampleSymbol) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }

func (p *Package) AllExamples() []*Example {
	if p.examples != nil {
		return p.examples
	}
	p.examples = make([]*Example, 0)
	p.addExamples(p, "", p.Doc.Examples)
	for _, f := range p.Doc.Funcs {
		p.addExamples(f, f.Name, f.Examples)
	}
	for _, t := range p.Doc.Types {
		p.addExamples(t, t.Name, t.Examples)
		for _, f := range t.Funcs {
			p.addExamples(f, f.Name, f.Examples)
		}
		for _, m := range t.Methods {
			if len(m.Examples) > 0 {
				p.addExamples(m, t.Name+"."+m.Name, m.Examples)
			}
		}
	}
	sort.Stable(byExampleSymbol(p.examples))
	return p.examples
}

func (p *Package) PackageExamples() []*Example {
	if p.examples == nil {
		p.AllExamples()
	}
	return p.ObjExamples(p)
}

func (p *Package) ObjExamples(obj interface{}) []*Example {
	var examples []*Example
	for _, e := range p.examples {
		if e.obj == obj {
			examples = append(examples, e)
		}
	}
	return examples
}

func (p *Package) Breadcrumbs(templateName string) htemp.HTML {
	modulePath := p.ModulePath
	if p.ImportPath == stdlib.ModulePath {
		return htemp.HTML(`<span class="text-muted">Standard library</span>`)
	}
	if !strings.HasPrefix(p.ImportPath, p.ModulePath) {
		// This is the case for stdlib packages
		modulePath = strings.SplitN(p.ImportPath, "/", 2)[0]
	}
	var buf bytes.Buffer
	i := 0
	j := len(modulePath)
	if j == 0 {
		j = strings.IndexRune(p.ImportPath, '/')
		if j < 0 {
			j = len(p.ImportPath)
		}
	}
	for {
		if i != 0 {
			buf.WriteString(`<span class="text-muted">/</span>`)
		}
		link := j < len(p.ImportPath) || templateName != "doc.html"
		if link {
			buf.WriteString(`<a href="`)
			buf.WriteString(formatPathFrag(p.ImportPath[:j], ""))
			buf.WriteString(p.VersionParam())
			buf.WriteString(p.PlatformParam())
			buf.WriteString(`">`)
		} else {
			buf.WriteString(`<span class="text-muted">`)
		}
		buf.WriteString(htemp.HTMLEscapeString(p.ImportPath[i:j]))
		if link {
			buf.WriteString("</a>")
		} else {
			buf.WriteString("</span>")
		}
		i = j + 1
		if i >= len(p.ImportPath) {
			break
		}
		j = strings.IndexRune(p.ImportPath[i:], '/')
		if j < 0 {
			j = len(p.ImportPath)
		} else {
			j += i
		}
	}
	return htemp.HTML(buf.String())
}

func formatPathFrag(path, fragment string) string {
	if len(path) > 0 && path[0] != '/' {
		path = "/" + path
	}
	u := url.URL{Path: path, Fragment: fragment}
	return u.String()
}

// relativePathFn formats an import path as HTML.
func relativePathFn(path string, parentPath interface{}) string {
	if p, ok := parentPath.(string); ok && p != "" && strings.HasPrefix(path, p+"/") {
		path = path[len(p)+1:]
	}
	return path
}

func (p *Package) Code(decl ast.Decl, typ *doc.Type) htemp.HTML {
	c := printer.PrintDecl(p.fset, decl)
	return p.code(c, typ)
}

func (p *Package) GeminiCode(node ast.Node) string {
	var out strings.Builder
	config := goprinter.Config{
		Mode:     goprinter.UseSpaces,
		Tabwidth: 4,
	}
	config.Fprint(&out, p.fset, node)
	return out.String()
}

var period = []byte{'.'}

func (p *Package) code(c printer.Code, typ *doc.Type) htemp.HTML {
	var buf bytes.Buffer
	last := 0
	src := []byte(c.Text)
	buf.WriteString("<pre>")
	for _, a := range c.Annotations {
		htemp.HTMLEscape(&buf, src[last:a.Pos])
		switch a.Kind {
		case printer.PackageLinkAnnotation:
			buf.WriteString(`<a href="`)
			buf.WriteString(formatPathFrag(c.Paths[a.PathIndex], ""))
			buf.WriteString(`">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</a>`)
		case printer.LinkAnnotation, printer.BuiltinAnnotation:
			var p string
			if a.Kind == printer.BuiltinAnnotation {
				p = "builtin"
			} else if a.PathIndex >= 0 {
				p = c.Paths[a.PathIndex]
			}
			n := src[a.Pos:a.End]
			n = n[bytes.LastIndex(n, period)+1:]
			buf.WriteString(`<a href="`)
			buf.WriteString(formatPathFrag(p, string(n)))
			buf.WriteString(`">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</a>`)
		case printer.CommentAnnotation:
			buf.WriteString(`<span class="com">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</span>`)
		case printer.AnchorAnnotation:
			buf.WriteString(`<span id="`)
			if typ != nil {
				htemp.HTMLEscape(&buf, []byte(typ.Name))
				buf.WriteByte('.')
			}
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</span>`)
		default:
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
		}
		last = int(a.End)
	}
	htemp.HTMLEscape(&buf, src[last:])
	buf.WriteString("</pre>")
	return htemp.HTML(buf.String())
}

func isInterfaceFn(t *doc.Type) bool {
	// TODO: Precompute this
	if t.Decl.Tok != token.TYPE {
		return false
	}
	isInterface := false
	for _, spec := range t.Decl.Specs {
		ts := spec.(*ast.TypeSpec)
		if t.Name != ts.Name.Name {
			continue
		}
		_, isInterface = ts.Type.(*ast.InterfaceType)
		break
	}
	return isInterface
}

type TemplateMap map[string]interface {
	Execute(io.Writer, interface{}) error
}

func (m TemplateMap) ExecuteHTML(resp http.ResponseWriter, name string, status int, data interface{}) error {
	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	return m.ExecuteHTTP(resp, name, status, data)
}

func (m TemplateMap) ExecuteHTTP(resp http.ResponseWriter, name string, status int, data interface{}) error {
	resp.WriteHeader(status)
	if status == http.StatusNotModified {
		return nil
	}
	return m.Execute(resp, name, data)
}

func (m TemplateMap) Execute(w io.Writer, name string, data interface{}) error {
	t := m[name]
	if t == nil {
		return fmt.Errorf("template %s not found", name)
	}
	return t.Execute(w, data)
}

func (m TemplateMap) ParseHTML(name string, funcs htemp.FuncMap, files ...string) error {
	t := htemp.New("").Funcs(funcs).Funcs(htemp.FuncMap{
		"templateName": func() string { return name },
	})
	if _, err := t.ParseFiles(files...); err != nil {
		return err
	}
	t = t.Lookup("ROOT")
	if t == nil {
		return fmt.Errorf("ROOT template not found in %v", files)
	}
	m[name] = t
	return nil
}

func (m TemplateMap) ParseText(name string, funcs ttemp.FuncMap, files ...string) error {
	t := ttemp.New(name).Funcs(funcs).Funcs(ttemp.FuncMap{
		"templateName": func() string { return name },
	})
	if _, err := t.ParseFiles(files...); err != nil {
		return err
	}
	m[name] = t
	return nil
}

func joinTemplateDir(base string, files []string) []string {
	result := make([]string, len(files))
	for i := range files {
		result[i] = filepath.Join(base, files[i])
	}
	return result
}

func (s *Server) parseHTMLTemplates(m TemplateMap, dir string, cb *httputil.CacheBusters) error {
	sets := [][]string{
		{"about.html", "common.html", "layout.html"},
		{"doc.html", "common.html", "layout.html"},
		{"index.html", "common.html", "layout.html"},
		{"versions.html", "common.html", "layout.html"},
		{"platforms.html", "common.html", "layout.html"},
		{"imports.html", "common.html", "layout.html"},
		{"notfound.html", "common.html", "layout.html"},
		{"search.html", "common.html", "layout.html"},
		{"tools.html", "common.html", "layout.html"},
		{"graph.html", "common.html"},
	}
	funcs := htemp.FuncMap{
		"equal":        reflect.DeepEqual,
		"isInterface":  isInterfaceFn,
		"relativePath": relativePathFn,
		"staticPath":   func(p string) string { return cb.AppendQueryParam(p, "v") },
		"humanize":     humanize.Time,
		"config":       func() *Config { return s.cfg },
	}
	for _, set := range sets {
		err := m.ParseHTML(set[0], funcs, joinTemplateDir(dir, set)...)
		if err != nil {
			return err
		}
	}
	tfuncs := ttemp.FuncMap{
		"config": func() *Config { return s.cfg },
	}
	err := m.ParseText("opensearch.xml", tfuncs, filepath.Join(dir, "opensearch.xml"))
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) parseGeminiTemplates(m TemplateMap, dir string) error {
	sets := [][]string{
		{"index.gmi"},
		{"about.gmi"},
		{"search.gmi"},
		{"doc.gmi"},
		{"versions.gmi"},
		{"platforms.gmi"},
		{"imports.gmi"},
	}
	funcs := ttemp.FuncMap{
		"relativePath": relativePathFn,
		"humanize":     humanize.Time,
		"config":       func() *Config { return s.cfg },
	}
	for _, set := range sets {
		err := m.ParseText(set[0], funcs, joinTemplateDir(dir, set)...)
		if err != nil {
			return err
		}
	}
	return nil
}
