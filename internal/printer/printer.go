// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

// Package printer implements annotated rendering of Go code.
package printer

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/format"
	"go/printer"
	"go/scanner"
	"go/token"
	"strconv"
)

type AnnotationKind int16

const (
	// Link to export in package specified by Paths[PathIndex] with fragment
	// Text[strings.LastIndex(Text[Pos:End], ".")+1:End].
	LinkAnnotation AnnotationKind = iota

	// Anchor with name specified by Text[Pos:End] or typeName + "." +
	// Text[Pos:End] for type declarations.
	AnchorAnnotation

	// Comment.
	CommentAnnotation

	// Link to package specified by Paths[PathIndex].
	PackageLinkAnnotation

	// Link to builtin entity with name Text[Pos:End].
	BuiltinAnnotation
)

type Annotation struct {
	Pos, End  int32
	Kind      AnnotationKind
	PathIndex int16
}

type Code struct {
	Text        string
	Annotations []Annotation
	Paths       []string
}

// declVisitor modifies a declaration AST for printing and collects annotations.
type declVisitor struct {
	annotations []Annotation
	paths       []string
	pathIndex   map[string]int
	comments    []*ast.CommentGroup
}

func (v *declVisitor) add(kind AnnotationKind, importPath string) {
	pathIndex := -1
	if importPath != "" {
		var ok bool
		pathIndex, ok = v.pathIndex[importPath]
		if !ok {
			pathIndex = len(v.paths)
			v.paths = append(v.paths, importPath)
			v.pathIndex[importPath] = pathIndex
		}
	}
	v.annotations = append(v.annotations, Annotation{Kind: kind, PathIndex: int16(pathIndex)})
}

func (v *declVisitor) ignoreName() {
	v.add(-1, "")
}

func (v *declVisitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.TypeSpec:
		v.ignoreName()
		if n.TypeParams != nil {
			ast.Walk(v, n.TypeParams)
		}
		switch n := n.Type.(type) {
		case *ast.InterfaceType:
			for _, f := range n.Methods.List {
				for range f.Names {
					v.add(AnchorAnnotation, "")
				}
				ast.Walk(v, f.Type)
			}
		case *ast.StructType:
			for _, f := range n.Fields.List {
				for range f.Names {
					v.add(AnchorAnnotation, "")
				}
				ast.Walk(v, f.Type)
			}
		default:
			ast.Walk(v, n)
		}
	case *ast.FuncDecl:
		if n.Recv != nil {
			ast.Walk(v, n.Recv)
		}
		v.ignoreName()
		ast.Walk(v, n.Type)
	case *ast.Field:
		for range n.Names {
			v.ignoreName()
		}
		ast.Walk(v, n.Type)
	case *ast.ValueSpec:
		for range n.Names {
			v.add(AnchorAnnotation, "")
		}
		if n.Type != nil {
			ast.Walk(v, n.Type)
		}
		for _, x := range n.Values {
			ast.Walk(v, x)
		}
	case *ast.Ident:
		switch {
		case n.Obj == nil && doc.IsPredeclared(n.Name):
			v.add(BuiltinAnnotation, "")
		case n.Obj != nil && ast.IsExported(n.Name):
			if _, ok := n.Obj.Decl.(*ast.TypeSpec); ok {
				v.add(LinkAnnotation, "")
			} else {
				v.ignoreName()
			}
		default:
			v.ignoreName()
		}
	case *ast.SelectorExpr:
		if x, _ := n.X.(*ast.Ident); x != nil {
			if obj := x.Obj; obj != nil && obj.Kind == ast.Pkg {
				if spec, _ := obj.Decl.(*ast.ImportSpec); spec != nil {
					if path, err := strconv.Unquote(spec.Path.Value); err == nil {
						v.add(PackageLinkAnnotation, path)
						if path == "C" {
							v.ignoreName()
						} else {
							v.add(LinkAnnotation, path)
						}
						return nil
					}
				}
			}
		}
		ast.Walk(v, n.X)
		v.ignoreName()
	case *ast.BasicLit:
		if n.Kind == token.STRING && len(n.Value) > 128 {
			v.comments = append(v.comments,
				&ast.CommentGroup{List: []*ast.Comment{{
					Slash: n.Pos(),
					Text:  fmt.Sprintf("/* %d byte string literal not displayed */", len(n.Value)),
				}}})
			n.Value = `""`
		} else {
			return v
		}
	case *ast.CompositeLit:
		if len(n.Elts) > 100 {
			if n.Type != nil {
				ast.Walk(v, n.Type)
			}
			v.comments = append(v.comments,
				&ast.CommentGroup{List: []*ast.Comment{{
					Slash: n.Lbrace,
					Text:  fmt.Sprintf("/* %d elements not displayed */", len(n.Elts)),
				}}})
			n.Elts = n.Elts[:0]
		} else {
			return v
		}
	default:
		return v
	}
	return nil
}

func newScanner(src []byte) (*scanner.Scanner, *token.File) {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s := &scanner.Scanner{}
	s.Init(file, src, nil, scanner.ScanComments)
	return s, file
}

// PrintDecl prints and annotates the given decl.
func PrintDecl(fset *token.FileSet, decl ast.Decl) Code {
	v := &declVisitor{pathIndex: make(map[string]int)}
	ast.Walk(v, decl)

	node := &printer.CommentedNode{
		Node:     decl,
		Comments: v.comments,
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return Code{Text: err.Error()}
	}

	src := buf.Bytes()
	s, file := newScanner(src)
	annotations := []Annotation{}
	prevTok := token.ILLEGAL
loop:
	for {
		pos, tok, lit := s.Scan()
		switch tok {
		case token.EOF:
			break loop
		case token.COMMENT:
			p := file.Offset(pos)
			e := p + len(lit)
			if prevTok == token.COMMENT {
				annotations[len(annotations)-1].End = int32(e)
			} else {
				annotations = append(annotations, Annotation{Kind: CommentAnnotation, Pos: int32(p), End: int32(e)})
			}
		case token.IDENT:
			if len(v.annotations) == 0 {
				// Oops!
				break loop
			}
			annotation := v.annotations[0]
			v.annotations = v.annotations[1:]
			if annotation.Kind == -1 {
				continue
			}
			p := file.Offset(pos)
			e := p + len(lit)
			annotation.Pos = int32(p)
			annotation.End = int32(e)
			annotations = append(annotations, annotation)
		}
		prevTok = tok
	}
	return Code{Text: string(src), Annotations: annotations, Paths: v.paths}
}

// PrintExample prints and annotates the given example.
func PrintExample(src string) Code {
	var annotations []Annotation
	s, file := newScanner([]byte(src))
	prevTok := token.ILLEGAL
scanLoop:
	for {
		pos, tok, lit := s.Scan()
		switch tok {
		case token.EOF:
			break scanLoop
		case token.COMMENT:
			p := file.Offset(pos)
			e := p + len(lit)
			if prevTok == token.COMMENT {
				annotations[len(annotations)-1].End = int32(e)
			} else {
				annotations = append(annotations, Annotation{Kind: CommentAnnotation, Pos: int32(p), End: int32(e)})
			}
		}
		prevTok = tok
	}

	return Code{Text: string(src), Annotations: annotations}
}
