package server

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/printer"
	"go/token"
	htemp "html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"text/template"

	"git.sr.ht/~sircmpwn/gddo/internal/autodiscovery"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/render"
)

// Renderer provides functions to render Go documentation.
type Renderer struct {
	fset    *token.FileSet
	parser  *comment.Parser
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
	return &Renderer{
		fset:    p.FileSet,
		parser:  p.Parser(),
		project: p.project,
		ref:     p.Reference,
		dir:     p.innerPath,

		version:      p.Version,
		platform:     p.Platform,
		showVersion:  p.Version != p.LatestVersion,
		showPlatform: p.Platform != cfg.Platform,
	}
}

// ExecuteHTML executes an HTML template.
func (r *Renderer) ExecuteHTML(t *htemp.Template, w http.ResponseWriter, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return htemp.Must(t.Clone()).Funcs(r.HTMLFuncs()).Execute(w, data)
}

// ExecuteHTTP executes an HTTP text template.
func (r *Renderer) ExecuteHTTP(t *template.Template, w io.Writer, data any) error {
	return t.Execute(w, data)
}

// HTMLFuncs returns a [template.FuncMap] for use in HTML templates.
func (r *Renderer) HTMLFuncs() template.FuncMap {
	return template.FuncMap{
		"render_doc":    r.DocHTML,
		"render_func":   r.FuncString,
		"render_decl":   r.DeclHTML,
		"render_code":   r.CodeHTML,
		"source_link":   r.SourceLink,
		"is_interface":  r.IsInterface,
		"play_id":       r.PlayID,
		"view":          r.View,
		"query":         r.Query,
		"breadcrumbs":   r.Breadcrumbs,
		"relative_path": relativePath,
		"platforms":     platformList,
	}
}

// DocHTML returns formatted HTML for the doc comment text.
func (r *Renderer) DocHTML(text string) htemp.HTML {
	return render.DocHTML(r.parser.Parse(text))
}

// FuncString formats a function declaration into a single line.
func (r *Renderer) FuncString(decl *ast.FuncDecl) string {
	var out strings.Builder
	config := printer.Config{
		Mode:     printer.UseSpaces,
		Tabwidth: 4,
	}
	config.Fprint(&out, r.fset, decl)
	return out.String()
}

// DeclHTML renders a Go declaration as HTML.
func (r *Renderer) DeclHTML(decl ast.Decl, typ *doc.Type) htemp.HTML {
	html, err := render.DeclHTML(r.fset, decl, typ)
	if err != nil {
		log.Printf("Error rendering ast.Decl: %v", err)
		return "<pre>Error rendering declaration code</pre>"
	}
	return html
}

// CodeHTML renders example code as HTML.
func (r *Renderer) CodeHTML(ex *doc.Example) htemp.HTML {
	html, err := render.CodeHTML(r.fset, ex)
	if err != nil {
		log.Printf("Error rendering *doc.Example: %v", err)
		return "<pre>Error rendering example code</pre>"
	}
	return html
}

// SourceLink returns a source link for the given position.
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

// IsInterface reports whether t is an interface type.
func (r *Renderer) IsInterface(t *doc.Type) bool {
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

// PlayID returns the play ID for the given example.
func (r *Renderer) PlayID(ex *Example) string {
	symbol := ex.Symbol
	if symbol == "" {
		symbol = "package"
	}
	return symbol + "-" + ex.Example.Suffix
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

// Breadcrumb provides a link back to a previous page.
type Breadcrumb struct {
	Text       string
	ImportPath string
	Current    bool
}

// Breadcrumbs computes breadcrumbs for the given package.
func (r *Renderer) Breadcrumbs(p *Package) []Breadcrumb {
	if p.ImportPath == proxy.StdlibModulePath {
		return nil
	}

	crumbs := []Breadcrumb{}
	importPath := p.ModulePath
	if p.ModulePath == proxy.StdlibModulePath {
		importPath = ""
	} else {
		crumbs = append(crumbs, Breadcrumb{
			Text:       p.ModulePath,
			ImportPath: p.ModulePath,
			Current:    p.ImportPath == p.ModulePath,
		})
	}
	if p.innerPath != "" {
		for _, part := range strings.Split(p.innerPath, "/") {
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

// relativePath returns a path relative to parentPath.
func relativePath(path, parentPath string) string {
	if parentPath != "" && strings.HasPrefix(path, parentPath+"/") {
		path = path[len(parentPath)+1:]
	}
	return path
}

// platformList returns the list of supported platforms.
func platformList() []string {
	return platforms
}
