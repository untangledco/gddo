package internal

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/token"

	"git.sr.ht/~sircmpwn/gddo/internal/codec"
)

// Package is a collection of Go source files.
type Package struct {
	Files []*File
	fset  *token.FileSet
}

// File represents a source file.
type File struct {
	Name string
	AST  *ast.File
}

// NewPackage returns a new empty package.
func NewPackage() *Package {
	return &Package{
		fset: token.NewFileSet(),
	}
}

// FileSet returns the AST FileSet for the package.
func (p *Package) FileSet() *token.FileSet {
	return p.fset
}
func (p *Package) FastEncode() ([]byte, error) {
	enc := codec.NewEncoder()
	fsb, err := fsetToBytes(p.fset)
	if err != nil {
		return nil, err
	}
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	if err := enc.Encode(fsb); err != nil {
		return nil, err
	}
	return enc.Bytes(), nil
}

func FastDecodePackage(data []byte) (*Package, error) {
	dec := codec.NewDecoder(data)
	x, err := dec.Decode()
	if err != nil {
		return nil, err
	}
	pkg, ok := x.(*Package)
	if !ok {
		return nil, fmt.Errorf("first decoded value is %T, wanted *Package", pkg)
	}
	if pkg == nil {
		// An empty package may be encoded as nil.
		pkg = &Package{}
	}
	x, err = dec.Decode()
	if err != nil {
		return nil, err
	}
	fsetBytes, ok := x.([]byte)
	if !ok {
		return nil, fmt.Errorf("second decoded value is %T, wanted []byte", fsetBytes)
	}
	fset, err := fsetFromBytes(fsetBytes)
	if err != nil {
		return nil, err
	}
	pkg.fset = fset
	return pkg, nil
}

// token.FileSet uses some unexported types in its encoding, so we can't use our
// own codec from it. Instead we use gob and encode the resulting bytes.
func fsetToBytes(fset *token.FileSet) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := fset.Write(enc.Encode); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fsetFromBytes(data []byte) (*token.FileSet, error) {
	dec := gob.NewDecoder(bytes.NewReader(data))
	fset := token.NewFileSet()
	if err := fset.Read(dec.Decode); err != nil {
		return nil, err
	}
	return fset, nil
}
