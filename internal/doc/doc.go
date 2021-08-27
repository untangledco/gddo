// Package doc renders Go package documentation.
package doc

import (
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"sort"

	"git.sr.ht/~sircmpwn/gddo/internal/source"
)

// Package is the documentation for an entire package.
type Package struct {
	Name      string
	Imports   []string
	Filenames []string
	Notes     map[string][]*Note
	Doc       string
	Synopsis  string
	Consts    []*Value
	Types     []*Type
	Vars      []*Value
	Funcs     []*Func
	Examples  []*Example
}

type Note struct {
	Pos  Pos
	UID  string
	Body string
}

type Value struct {
	Decl Code
	Pos  Pos
	Doc  string
}

type Type struct {
	Doc      string
	Name     string
	Decl     Code
	Pos      Pos
	Consts   []*Value
	Vars     []*Value
	Funcs    []*Func
	Methods  []*Func
	Examples []*Example
}

type Func struct {
	Decl     Code
	Pos      Pos
	Doc      string
	Name     string
	Recv     string // actual receiver "T" or "*T"
	Orig     string // original receiver "T" or "*T"
	Examples []*Example
}

type Example struct {
	Name   string
	Doc    string
	Code   Code
	Play   string
	Output string
}

type File struct {
	Name string
	URL  string
}

type Pos struct {
	Line int32  // 0 if not valid.
	N    uint16 // number of lines - 1
	File int16  // index in Package.Filenames
}

// New computes documentation for the given package.
func New(src *source.Package, ctx *build.Context) (*Package, error) {
	ctx = src.BuildContext(ctx)

	// Sort and index files
	b := &builder{
		files: map[string]*file{},
		fset:  token.NewFileSet(),
	}
	var names []string
	for _, f := range src.Files {
		if match, _ := ctx.MatchFile(".", f.Name); !match {
			continue
		}
		names = append(names, f.Name)
		b.files[f.Name] = &file{
			Name:     f.Name,
			Contents: f.Contents,
		}
	}
	sort.Strings(names)

	// Parse the files
	var files []*ast.File
	for _, name := range names {
		file, err := parser.ParseFile(b.fset, name, b.files[name].Contents, parser.ParseComments)
		if err != nil {
			return nil, err
		} else {
			files = append(files, file)
		}
	}

	mode := doc.Mode(0)
	if src.Path == "builtin" {
		mode |= doc.AllDecls
	}

	pkg, err := doc.NewFromFiles(b.fset, files, src.Path, mode)
	if err != nil {
		return nil, err
	}

	for i := range pkg.Filenames {
		b.files[pkg.Filenames[i]].Index = i
	}

	if pkg.ImportPath == "builtin" {
		removeAssociations(pkg)
	}

	return &Package{
		Name:      pkg.Name,
		Imports:   pkg.Imports,
		Filenames: pkg.Filenames,
		Notes:     b.notes(pkg.Notes),
		Doc:       pkg.Doc,
		Synopsis:  doc.Synopsis(pkg.Doc),
		Consts:    b.values(pkg.Consts),
		Types:     b.types(pkg.Types),
		Vars:      b.values(pkg.Vars),
		Funcs:     b.funcs(pkg.Funcs),
		Examples:  b.examples(pkg.Examples),
	}, nil
}
