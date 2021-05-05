// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package doc

import (
	"go/doc"
	"go/format"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// builder holds the state used when building the documentation.
type builder struct {
	files map[string]*file
	fset  *token.FileSet
	buf   []byte // scratch space for printNode method.
}

type file struct {
	Name     string
	Contents []byte
	Index    int
}

type byFuncName []*doc.Func

func (s byFuncName) Len() int           { return len(s) }
func (s byFuncName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byFuncName) Less(i, j int) bool { return s[i].Name < s[j].Name }

func removeAssociations(dpkg *doc.Package) {
	for _, t := range dpkg.Types {
		dpkg.Funcs = append(dpkg.Funcs, t.Funcs...)
		t.Funcs = nil
	}
	sort.Sort(byFuncName(dpkg.Funcs))
}

func (b *builder) values(vdocs []*doc.Value) []*Value {
	var result []*Value
	for _, d := range vdocs {
		result = append(result, &Value{
			Decl: b.printDecl(d.Decl),
			Pos:  b.position(d.Decl),
			Doc:  d.Doc,
		})
	}
	return result
}

type posNode token.Pos

func (p posNode) Pos() token.Pos { return token.Pos(p) }
func (p posNode) End() token.Pos { return token.Pos(p) }

func (b *builder) notes(gnotes map[string][]*doc.Note) map[string][]*Note {
	if len(gnotes) == 0 {
		return nil
	}
	notes := make(map[string][]*Note)
	for tag, gvalues := range gnotes {
		values := make([]*Note, len(gvalues))
		for i := range gvalues {
			values[i] = &Note{
				Pos:  b.position(posNode(gvalues[i].Pos)),
				UID:  gvalues[i].UID,
				Body: strings.TrimSpace(gvalues[i].Body),
			}
		}
		notes[tag] = values
	}
	return notes
}

var exampleOutputRx = regexp.MustCompile(`(?i)//[[:space:]]*output:`)

func (b *builder) examples(examples []*doc.Example) []*Example {
	var docs []*Example
	for _, e := range examples {
		code, output := b.printExample(e)

		play := ""
		if e.Play != nil {
			b.buf = b.buf[:0]
			if err := format.Node(sliceWriter{&b.buf}, b.fset, e.Play); err != nil {
				play = err.Error()
			} else {
				play = string(b.buf)
			}
		}

		docs = append(docs, &Example{
			Name:   strings.Title(e.Suffix),
			Doc:    e.Doc,
			Code:   code,
			Output: output,
			Play:   play,
		})
	}
	return docs
}

func (b *builder) funcs(fdocs []*doc.Func) []*Func {
	var result []*Func
	for _, d := range fdocs {
		result = append(result, &Func{
			Decl:     b.printDecl(d.Decl),
			Pos:      b.position(d.Decl),
			Doc:      d.Doc,
			Name:     d.Name,
			Recv:     d.Recv,
			Orig:     d.Orig,
			Examples: b.examples(d.Examples),
		})
	}
	return result
}

func (b *builder) types(tdocs []*doc.Type) []*Type {
	var result []*Type
	for _, d := range tdocs {
		result = append(result, &Type{
			Doc:      d.Doc,
			Name:     d.Name,
			Decl:     b.printDecl(d.Decl),
			Pos:      b.position(d.Decl),
			Consts:   b.values(d.Consts),
			Vars:     b.values(d.Vars),
			Funcs:    b.funcs(d.Funcs),
			Methods:  b.funcs(d.Methods),
			Examples: b.examples(d.Examples),
		})
	}
	return result
}
