package server

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/format"
	goprinter "go/printer"
	"go/token"
	htemp "html/template"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"text/template"

	"git.sr.ht/~sircmpwn/gddo/internal/autodiscovery"
	"git.sr.ht/~sircmpwn/gddo/internal/gemini"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/printer"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// Renderer provides functions to render Go documentation.
type Renderer struct {
	fset    *token.FileSet
	parser  *comment.Parser
	printer *comment.Printer
	project *autodiscovery.Project
	ref     string
	dir     string

	version      string
	platform     string
	showVersion  bool
	showPlatform bool
}

// NewRenderer returns a new renderer for the given package.
func NewRenderer(p *Package, cfg *Config) *Renderer {
	printer := p.Printer()
	printer.HeadingLevel = 4

	return &Renderer{
		fset:    p.FileSet,
		parser:  p.Parser(),
		printer: printer,
		project: p.project,
		ref:     p.Reference,
		dir:     p.dir,

		version:      p.Version,
		platform:     p.Platform,
		showVersion:  p.Version != p.LatestVersion,
		showPlatform: p.Platform != cfg.Platform,
	}
}

// ExecuteHTML executes an HTML template.
func (r *Renderer) ExecuteHTML(t *htemp.Template, w io.Writer, data any) error {
	return htemp.Must(t.Clone()).Funcs(r.HTMLFuncs()).Execute(w, data)
}

// ExecuteHTTP executes an HTTP text template.
func (r *Renderer) ExecuteHTTP(t *template.Template, w io.Writer, data any) error {
	return t.Execute(w, data)
}

// ExecuteGemini executes a Gemini text template.
func (r *Renderer) ExecuteGemini(t *template.Template, w io.Writer, data any) error {
	return template.Must(t.Clone()).Funcs(r.GeminiFuncs()).Execute(w, data)
}

// HTMLFuncs returns a [template.FuncMap] for use in HTML templates.
func (r *Renderer) HTMLFuncs() template.FuncMap {
	return template.FuncMap{
		"render_doc":    r.DocHTML,
		"render_func":   r.FuncString,
		"render_decl":   r.DeclHTML,
		"render_code":   r.CodeHTML,
		"source_link":   r.SourceLink,
		"breadcrumbs":   r.Breadcrumbs,
		"view":          r.View,
		"query":         r.Query,
		"relative_path": relativePath,
		"is_interface":  isInterface,
		"play_id":       playID,
		"platforms":     platforms.Platforms,
	}
}

// GeminiFuncs returns a [template.FuncMap] for use in Gemini templates.
func (r *Renderer) GeminiFuncs() template.FuncMap {
	return template.FuncMap{
		"render_doc":    r.DocGemini,
		"render_decl":   r.DeclGemini,
		"render_code":   r.CodeGemini,
		"view":          r.View,
		"query":         r.Query,
		"relative_path": relativePath,
		"platforms":     platforms.Platforms,
	}
}

// SourceLink returns a source link for given node.
func (r *Renderer) SourceLink(p token.Pos, text string) htemp.HTML {
	pos := r.fset.Position(p)
	if r.project == nil || pos.Line == 0 {
		return htemp.HTML(htemp.HTMLEscapeString(text))
	}
	link := r.project.LineURL(r.ref, r.dir, pos.Filename, strconv.Itoa(pos.Line))
	return htemp.HTML(fmt.Sprintf(`<a title="View Source" rel="noopener nofollow" href="%s">%s</a>`,
		htemp.HTMLEscapeString(link),
		htemp.HTMLEscapeString(text)))
}

// DocHTML returns formatted HTML for the doc comment text.
func (r *Renderer) DocHTML(text string) htemp.HTML {
	return htemp.HTML(r.printer.HTML(r.parser.Parse(text)))
}

// DocGemini returns formatted Gemini content for the doc comment text.
func (r *Renderer) DocGemini(text string) string {
	return string(gemini.Print(r.parser.Parse(text)))
}

// FuncString formats a function declaration into a single line.
func (r *Renderer) FuncString(decl *ast.FuncDecl) string {
	var out strings.Builder
	config := goprinter.Config{
		Mode:     goprinter.UseSpaces,
		Tabwidth: 4,
	}
	config.Fprint(&out, r.fset, decl)
	return out.String()
}

// isInterface reports whether t is an interface type.
func isInterface(t *doc.Type) bool {
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

// View returns a link for the current package.
func (r *Renderer) View(importPath, view string) string {
	var b strings.Builder
	if importPath != "" {
		b.WriteByte('/')
		b.WriteString(importPath)
		if r.showVersion {
			b.WriteByte('@')
			b.WriteString(r.version)
		}
	}
	if view != "" || r.showPlatform {
		b.WriteByte('?')
	}
	amp := false
	if view != "" {
		b.WriteString("view=")
		b.WriteString(view)
		amp = true
	}
	if r.showPlatform {
		if amp {
			b.WriteByte('&')
		}
		b.WriteString("platform=")
		b.WriteString(url.QueryEscape(r.platform))
	}
	return b.String()
}

// Query returns the current query, if necessary.
func (r *Renderer) Query() string {
	if !r.showPlatform {
		return ""
	}
	var b strings.Builder
	b.WriteString("?platform=")
	b.WriteString(url.QueryEscape(r.platform))
	return b.String()
}

// playID returns the play ID for the given example.
func playID(ex *Example) string {
	return ex.Symbol + "-" + ex.Example.Suffix
}

// Breadcrumb provides a link back to a previous page.
type Breadcrumb struct {
	Text       string
	ImportPath string
	Current    bool
}

// Breadcrumbs computes breadcrumbs for the given package.
func (r *Renderer) Breadcrumbs(p *Package) []Breadcrumb {
	if p.ImportPath == stdlib.ModulePath {
		return nil
	}

	crumbs := []Breadcrumb{}
	importPath := p.ModulePath
	if p.ModulePath == stdlib.ModulePath {
		importPath = ""
	} else {
		crumbs = append(crumbs, Breadcrumb{
			Text:       p.ModulePath,
			ImportPath: p.ModulePath,
			Current:    p.ImportPath == p.ModulePath,
		})
	}
	if p.dir != "" {
		for _, part := range strings.Split(p.dir, "/") {
			importPath = path.Join(importPath, part)
			crumbs = append(crumbs, Breadcrumb{
				Text:       part,
				ImportPath: importPath,
				Current:    p.ImportPath == importPath,
			})
		}
	}
	return crumbs
}

func formatPathFrag(path, fragment string) string {
	if len(path) > 0 && path[0] != '/' {
		path = "/" + path
	}
	u := url.URL{Path: path, Fragment: fragment}
	return u.String()
}

// relativePath returns a path relative to parentPath.
func relativePath(path, parentPath string) string {
	if parentPath != "" && strings.HasPrefix(path, parentPath+"/") {
		path = path[len(parentPath)+1:]
	}
	return path
}

// DeclHTML renders a Go declaration as HTML.
func (r *Renderer) DeclHTML(decl ast.Decl, typ *doc.Type) htemp.HTML {
	c := printer.PrintDecl(r.fset, decl)
	return codeToHTML(c, typ)
}

// DeclGemini renders a Go declaration as Gemini text.
func (r *Renderer) DeclGemini(decl ast.Decl) string {
	var buf strings.Builder
	if err := format.Node(&buf, r.fset, decl); err != nil {
		return err.Error()
	}
	return buf.String()
}

// codeString renders example code as a string.
func (r *Renderer) codeString(ex *doc.Example) (string, error) {
	var node interface{}
	if ex.Play != nil {
		node = ex.Play
	} else {
		node = &goprinter.CommentedNode{
			Node:     ex.Code,
			Comments: ex.Comments,
		}
	}
	var buf strings.Builder
	if err := format.Node(&buf, r.fset, node); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// CodeHTML renders example code as HTML.
func (r *Renderer) CodeHTML(ex *doc.Example) (htemp.HTML, error) {
	var c printer.Code
	codeStr, err := r.codeString(ex)
	if err != nil {
		c = printer.Code{Text: err.Error()}
	} else {
		c = printer.PrintExample(codeStr)
	}
	return codeToHTML(c, nil), nil
}

// CodeGemini renders example code as Gemini text.
func (r *Renderer) CodeGemini(ex *doc.Example) (string, error) {
	return r.codeString(ex)
}

var period = []byte{'.'}

// codeToHTML converts annotated Go code to HTML.
func codeToHTML(c printer.Code, typ *doc.Type) htemp.HTML {
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
