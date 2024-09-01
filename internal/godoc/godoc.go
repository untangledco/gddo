// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package godoc is for rendering Go documentation.
package godoc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strings"
)

// A Package contains package-level information needed to render Go documentation.
type Package struct {
	Fset *token.FileSet
	encPackage
}

// encPackage holds the fields of Package that can be directly encoded.
//
// If you add any fields, run go generate on this package.
//
// If you remove a field, you will have to hand-edit encode_ast.go
// just to make this package compile before running go generate.
// Do not remove the field name from the "Fields of" comment, however,
// or existing encoded data will decode incorrectly.
//
// If you rename a field, the field's values in existing encoded
// data will be lost. Renaming a field is like deleting the old field
// and adding a new one.
type encPackage struct { // fields that can be directly encoded
	Files []*File
}

// A File contains everything needed about a source file to render documentation.
type File struct {
	Name string // full file pathname relative to zip content directory
	AST  *ast.File
}

// AddFile adds a file to the Package. After it returns, the contents of the ast.File
// are unsuitable for anything other than the methods of this package.
func (p *Package) AddFile(f *ast.File, removeNodes bool) {
	filename := p.Fset.Position(f.Package).Filename
	// Don't trim anything from a test file or one in a XXX_test package; it
	// may be part of a playable example.
	if removeNodes && !strings.HasSuffix(filename, "_test.go") && !strings.HasSuffix(f.Name.Name, "_test") {
		removeUnusedASTNodes(f)
	}
	p.Files = append(p.Files, &File{
		Name: filename,
		AST:  f,
	})
}

// removeUnusedASTNodes removes parts of the AST not needed for documentation.
// It doesn't remove unexported consts, vars or types, although it probably could.
func removeUnusedASTNodes(pf *ast.File) {
	var decls []ast.Decl
	for _, d := range pf.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			// Remove all unexported functions and function bodies.
			if f.Name == nil || !ast.IsExported(f.Name.Name) {
				continue
			}
			// Remove the function body, unless it's an example.
			// The doc contains example bodies.
			if !strings.HasPrefix(f.Name.Name, "Example") {
				f.Body = nil
			}
		}
		decls = append(decls, d)
	}
	// Don't remove pf.Comments; they may contain Notes.
	pf.Decls = decls
}

// ParseFiles parses each abstract syntax tree from the named files in fsys.
// The returned package contains...
func ParseFiles(fsys fs.FS, names []string, isBuiltin bool) (*Package, error) {
	pkg := &Package{Fset: token.NewFileSet()}
	for _, name := range names {
		f, err := fsys.Open(name)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(pkg.Fset, path.Base(name), f, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		f.Close()

		// Don't strip the seemingly unexported functions from the builtin package;
		// they are actually Go builtins like make, new, etc.
		removeUnused := !isBuiltin
		pkg.AddFile(file, removeUnused)
	}
	return pkg, nil
}
