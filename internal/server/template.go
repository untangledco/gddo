// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package server

import (
	"bytes"
	"fmt"
	godoc "go/doc"
	htemp "html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	ttemp "text/template"

	"github.com/dustin/go-humanize"

	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

type texample struct {
	ID      string
	Label   string
	Example *doc.Example
	Play    bool
	obj     interface{}
}

func (pkg *Package) View(view string) string {
	var b strings.Builder
	b.WriteByte('?')
	amp := false
	if len(view) != 0 {
		b.WriteString("view=")
		b.WriteString(view)
		amp = true
	}
	if pkg.platformParam {
		if amp {
			b.WriteByte('&')
		}
		b.WriteString("platform=")
		b.WriteString(url.QueryEscape(pkg.Platform))
	}
	return b.String()
}

func (pkg *Package) PlatformParam() string {
	if !pkg.platformParam {
		return ""
	}
	var b strings.Builder
	b.WriteString("?platform=")
	b.WriteString(url.QueryEscape(pkg.Platform))
	return b.String()
}

func (pkg *Package) VersionParam() string {
	if pkg.Version == pkg.LatestVersion {
		return ""
	}
	return "@" + pkg.Version
}

func (pkg *Package) SourceLink(pos doc.Pos, text string, textOnlyOK bool) htemp.HTML {
	if pos.Line == 0 || pkg.Project == nil {
		if textOnlyOK {
			return htemp.HTML(htemp.HTMLEscapeString(text))
		}
		return ""
	}
	link := pkg.Project.Line(pkg.Reference, pkg.Dir, pkg.Filenames[pos.File],
		strconv.Itoa(int(pos.Line)))
	return htemp.HTML(fmt.Sprintf(`<a title="View Source" rel="noopener nofollow" href="%s">%s</a>`,
		htemp.HTMLEscapeString(link),
		htemp.HTMLEscapeString(text)))
}

func (pkg *Package) PageName() string {
	if pkg.Name != "" && pkg.Name != "main" {
		return pkg.Name
	}
	return path.Base(pkg.ImportPath)
}

func (pkg *Package) IsCommand() bool {
	return pkg.Name == "main"
}

func (pkg *Package) addExamples(obj interface{}, export, method string, examples []*doc.Example) {
	label := export
	id := export
	if method != "" {
		label += "." + method
		id += "-" + method
	}
	for _, e := range examples {
		te := &texample{
			Label:   label,
			ID:      id,
			Example: e,
			obj:     obj,
			// Only show play links for packages within the standard library.
			Play: e.Play != "" && stdlib.Contains(pkg.ImportPath),
		}
		if e.Name != "" {
			te.Label += " (" + e.Name + ")"
			if method == "" {
				te.ID += "-"
			}
			te.ID += "-" + e.Name
		}
		pkg.allExamples = append(pkg.allExamples, te)
	}
}

type byExampleID []*texample

func (e byExampleID) Len() int           { return len(e) }
func (e byExampleID) Less(i, j int) bool { return e[i].ID < e[j].ID }
func (e byExampleID) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }

func (pkg *Package) AllExamples() []*texample {
	if pkg.allExamples != nil {
		return pkg.allExamples
	}
	pkg.allExamples = make([]*texample, 0)
	pkg.addExamples(pkg, "package", "", pkg.Examples)
	for _, f := range pkg.Funcs {
		pkg.addExamples(f, f.Name, "", f.Examples)
	}
	for _, t := range pkg.Types {
		pkg.addExamples(t, t.Name, "", t.Examples)
		for _, f := range t.Funcs {
			pkg.addExamples(f, f.Name, "", f.Examples)
		}
		for _, m := range t.Methods {
			if len(m.Examples) > 0 {
				pkg.addExamples(m, t.Name, m.Name, m.Examples)
			}
		}
	}
	sort.Sort(byExampleID(pkg.allExamples))
	return pkg.allExamples
}

func (pkg *Package) PackageExamples() []*texample {
	if pkg.allExamples == nil {
		pkg.AllExamples()
	}
	return pkg.ObjExamples(pkg)
}

func (pkg *Package) ObjExamples(obj interface{}) []*texample {
	var examples []*texample
	for _, e := range pkg.allExamples {
		if e.obj == obj {
			examples = append(examples, e)
		}
	}
	return examples
}

func (pkg *Package) Breadcrumbs(templateName string) htemp.HTML {
	modulePath := pkg.ModulePath
	if !strings.HasPrefix(pkg.ImportPath, pkg.ModulePath) {
		return ""
	}
	var buf bytes.Buffer
	i := 0
	j := len(modulePath)
	if j == 0 {
		j = strings.IndexRune(pkg.ImportPath, '/')
		if j < 0 {
			j = len(pkg.ImportPath)
		}
	}
	for {
		if i != 0 {
			buf.WriteString(`<span class="text-muted">/</span>`)
		}
		link := j < len(pkg.ImportPath) || templateName != "doc.html"
		if link {
			buf.WriteString(`<a href="`)
			buf.WriteString(formatPathFrag(pkg.ImportPath[:j], ""))
			buf.WriteString(`">`)
		} else {
			buf.WriteString(`<span class="text-muted">`)
		}
		buf.WriteString(htemp.HTMLEscapeString(pkg.ImportPath[i:j]))
		if link {
			buf.WriteString("</a>")
		} else {
			buf.WriteString("</span>")
		}
		i = j + 1
		if i >= len(pkg.ImportPath) {
			break
		}
		j = strings.IndexRune(pkg.ImportPath[i:], '/')
		if j < 0 {
			j = len(pkg.ImportPath)
		} else {
			j += i
		}
	}
	return htemp.HTML(buf.String())
}

func (pkg *Package) Cgo() bool {
	for i := range pkg.Imports {
		if pkg.Imports[i] == "C" {
			return true
		}
	}
	return false
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
	if p, ok := parentPath.(string); ok && p != "" && strings.HasPrefix(path, p) {
		path = path[len(p)+1:]
	}
	return path
}

var (
	h3Pat  = regexp.MustCompile(`<h3 id="([^"]+)">([^<]+)</h3>`)
	rfcPat = regexp.MustCompile(`RFC\s+(\d{3,4})(,?\s+[Ss]ection\s+(\d+(\.\d+)*))?`)
)

func replaceAll(src []byte, re *regexp.Regexp, replace func(out, src []byte, m []int) []byte) []byte {
	var out []byte
	for len(src) > 0 {
		m := re.FindSubmatchIndex(src)
		if m == nil {
			break
		}
		out = append(out, src[:m[0]]...)
		out = replace(out, src, m)
		src = src[m[1]:]
	}
	if out == nil {
		return src
	}
	return append(out, src...)
}

// commentFn formats a source code comment as HTML.
func commentFn(v string) htemp.HTML {
	var buf bytes.Buffer
	godoc.ToHTML(&buf, v, nil)
	p := buf.Bytes()
	p = replaceAll(p, h3Pat, func(out, src []byte, m []int) []byte {
		out = append(out, `<h4 id="`...)
		out = append(out, src[m[2]:m[3]]...)
		out = append(out, `">`...)
		out = append(out, src[m[4]:m[5]]...)
		out = append(out, ` <a class="permalink" href="#`...)
		out = append(out, src[m[2]:m[3]]...)
		out = append(out, `">&para</a></h4>`...)
		return out
	})
	p = replaceAll(p, rfcPat, func(out, src []byte, m []int) []byte {
		out = append(out, `<a href="https://tools.ietf.org/html/rfc`...)
		out = append(out, src[m[2]:m[3]]...)

		// If available, add section fragment
		if m[4] != -1 {
			out = append(out, `#section-`...)
			out = append(out, src[m[6]:m[7]]...)
		}

		out = append(out, `">`...)
		out = append(out, src[m[0]:m[1]]...)
		out = append(out, `</a>`...)
		return out
	})
	return htemp.HTML(p)
}

var period = []byte{'.'}

func codeFn(c doc.Code, typ *doc.Type) htemp.HTML {
	var buf bytes.Buffer
	last := 0
	src := []byte(c.Text)
	buf.WriteString("<pre>")
	for _, a := range c.Annotations {
		htemp.HTMLEscape(&buf, src[last:a.Pos])
		switch a.Kind {
		case doc.PackageLinkAnnotation:
			buf.WriteString(`<a href="`)
			buf.WriteString(formatPathFrag(c.Paths[a.PathIndex], ""))
			buf.WriteString(`">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</a>`)
		case doc.LinkAnnotation, doc.BuiltinAnnotation:
			var p string
			if a.Kind == doc.BuiltinAnnotation {
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
		case doc.CommentAnnotation:
			buf.WriteString(`<span class="com">`)
			htemp.HTMLEscape(&buf, src[a.Pos:a.End])
			buf.WriteString(`</span>`)
		case doc.AnchorAnnotation:
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

var isInterfacePat = regexp.MustCompile(`^type [^ ]+ interface`)

func isInterfaceFn(t *doc.Type) bool {
	return isInterfacePat.MatchString(t.Decl.Text)
}

func noteTitleFn(s string) string {
	return strings.Title(strings.ToLower(s))
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

func parseHTMLTemplates(m TemplateMap, dir string, cb *httputil.CacheBusters) error {
	sets := [][]string{
		{"about.html", "common.html", "layout.html"},
		{"doc.html", "common.html", "layout.html"},
		{"index.html", "common.html", "layout.html"},
		{"versions.html", "common.html", "layout.html"},
		{"imports.html", "common.html", "layout.html"},
		{"notfound.html", "common.html", "layout.html"},
		{"search.html", "common.html", "layout.html"},
		{"std.html", "common.html", "layout.html"},
		{"tools.html", "common.html", "layout.html"},
		{"graph.html", "common.html"},
	}
	funcs := htemp.FuncMap{
		"code":         codeFn,
		"comment":      commentFn,
		"equal":        reflect.DeepEqual,
		"isInterface":  isInterfaceFn,
		"relativePath": relativePathFn,
		"staticPath":   func(p string) string { return cb.AppendQueryParam(p, "v") },
		"humanize":     humanize.Time,
	}
	for _, set := range sets {
		err := m.ParseHTML(set[0], funcs, joinTemplateDir(dir, set)...)
		if err != nil {
			return err
		}
	}
	err := m.ParseText("opensearch.xml", nil, filepath.Join(dir, "opensearch.xml"))
	if err != nil {
		return err
	}
	return nil
}

func parseGeminiTemplates(m TemplateMap, dir string) error {
	sets := [][]string{
		{"index.gmi"},
		{"about.gmi"},
		{"search.gmi"},
		{"doc.gmi"},
		{"versions.gmi"},
		{"imports.gmi"},
		{"std.gmi"},
	}
	funcs := ttemp.FuncMap{
		"comment": func(s string) string {
			var b strings.Builder
			doc.ToGemini(&b, s)
			return b.String()
		},
		"relativePath": relativePathFn,
		"humanize":     humanize.Time,
	}
	for _, set := range sets {
		err := m.ParseText(set[0], funcs, joinTemplateDir(dir, set)...)
		if err != nil {
			return err
		}
	}
	return nil
}
