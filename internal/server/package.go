package server

import (
	"go/ast"
	"go/doc"
	"go/token"
	"path"
	"sort"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/autodiscovery"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/godoc"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// Package is a [doc.Package] with additional information for use in templates.
type Package struct {
	*internal.Module
	*doc.Package

	FileSet     *token.FileSet
	Synopsis    string
	Platform    string
	SubPackages []database.Package
	Imported    []database.Package
	Message     string

	project     *autodiscovery.Project
	innerPath   string
	examples    []*Example
	examplesMap map[any][]*Example
}

// NewPackage returns a new package for use in templates.
func NewPackage(mod *internal.Module, platform, importPath string, src *godoc.Package) (*Package, error) {
	// Compute inner path
	innerPath := strings.TrimPrefix(importPath, mod.ModulePath)
	innerPath = strings.TrimPrefix(innerPath, "/")

	if src == nil {
		// A directory with no Go files
		docPkg := &doc.Package{
			ImportPath: importPath,
		}
		return &Package{
			Module:    mod,
			Package:   docPkg,
			Platform:  platform,
			innerPath: innerPath,
		}, nil
	}

	// Build documentation
	docPkg, err := buildDoc(importPath, src)
	if err != nil {
		return nil, err
	}

	pkg := &Package{
		Module:    mod,
		Package:   docPkg,
		FileSet:   src.Fset,
		Synopsis:  docPkg.Synopsis(docPkg.Doc),
		Platform:  platform,
		innerPath: innerPath,
	}
	pkg.collectExamples()
	return pkg, nil
}

// buildDoc builds documentation for the given package.
func buildDoc(importPath string, src *godoc.Package) (*doc.Package, error) {
	var files []*ast.File
	for _, f := range src.Files {
		files = append(files, f.AST)
	}
	mode := doc.Mode(0)
	if importPath == "builtin" {
		mode |= doc.AllDecls
	}
	pkg, err := doc.NewFromFiles(src.Fset, files, importPath, mode)
	if err != nil {
		return nil, err
	}
	if importPath == "builtin" {
		// Remove type associations
		for _, t := range pkg.Types {
			pkg.Funcs = append(pkg.Funcs, t.Funcs...)
			t.Funcs = nil
		}
		sort.Slice(pkg.Funcs, func(i, j int) bool {
			return pkg.Funcs[i].Name < pkg.Funcs[j].Name
		})
	}
	return pkg, nil
}

// Title returns a title for the package.
func (p *Package) Title() string {
	if p.ImportPath == stdlib.ModulePath {
		return "Standard library"
	}
	if p.IsPackage() {
		return "package " + p.Name
	}
	if p.Name == "main" {
		// Command
		return path.Base(p.ImportPath) + " command"
	}
	// Directory
	return path.Base(p.ImportPath) + "/ directory"
}

// ModuleTitle returns a title for the module.
func (p *Package) ModuleTitle() string {
	if p.ModulePath == stdlib.ModulePath {
		return "Standard library"
	}
	return path.Base(p.ModulePath)
}

// IsPackage reports whether p is a regular package.
func (p *Package) IsPackage() bool {
	return p.Name != "" && p.Name != "main"
}

// SummaryURL returns the URL for the project summary.
func (p *Package) SummaryURL() string {
	if p.project != nil {
		return p.project.Summary
	}
	return ""
}

// DirURL returns the URL for the package directory.
func (p *Package) DirURL() string {
	if p.project != nil {
		return p.project.DirURL(p.Reference, p.innerPath)
	}
	return ""
}

// FileURL returns the URL for the given file.
func (p *Package) FileURL(file string) string {
	if p.project != nil {
		return p.project.FileURL(p.Reference, p.innerPath, file)
	}
	return ""
}

// Example is a [doc.Example] with additional information for use in templates.
type Example struct {
	*doc.Example
	ID     string
	Symbol string
	Suffix string
	pkg    *Package
}

// collectExamples extracts examples into the internal examples representation.
func (p *Package) collectExamples() {
	p.examplesMap = make(map[any][]*Example)
	p.addExamples(p, "", p.Examples)
	for _, f := range p.Funcs {
		p.addExamples(f, f.Name, f.Examples)
	}
	for _, t := range p.Types {
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
	sort.SliceStable(p.examples, func(i, j int) bool {
		return p.examples[i].Symbol < p.examples[j].Symbol
	})
}

func (p *Package) addExamples(obj any, symbol string, examples []*doc.Example) {
	for _, example := range examples {
		suffix := strings.Title(example.Suffix)
		ex := &Example{
			Example: example,
			ID:      exampleID(symbol, suffix),
			Symbol:  symbol,
			Suffix:  suffix,
			pkg:     p,
		}

		p.examples = append(p.examples, ex)
		p.examplesMap[obj] = append(p.examplesMap[obj], ex)
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

// AllExamples returns a list of all examples.
func (p *Package) AllExamples() []*Example {
	return p.examples
}

// PackageExamples returns a list of examples associated with the package.
func (p *Package) PackageExamples() []*Example {
	return p.ObjExamples(p)
}

// ObjExamples returns a list of examples for the given object.
func (p *Package) ObjExamples(obj any) []*Example {
	return p.examplesMap[obj]
}
