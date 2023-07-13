// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"go/ast"
	"log"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/codec"
)

func main() {
	types := []any{
		ast.ArrayType{},
		ast.AssignStmt{},
		ast.BadDecl{},
		ast.BadExpr{},
		ast.BadStmt{},
		ast.BasicLit{},
		ast.BinaryExpr{},
		ast.BlockStmt{},
		ast.BranchStmt{},
		ast.CallExpr{},
		ast.CaseClause{},
		ast.ChanType{},
		ast.CommClause{},
		ast.CommentGroup{},
		ast.Comment{},
		ast.CompositeLit{},
		ast.DeclStmt{},
		ast.DeferStmt{},
		ast.Ellipsis{},
		ast.EmptyStmt{},
		ast.ExprStmt{},
		ast.FieldList{},
		ast.Field{},
		ast.ForStmt{},
		ast.FuncDecl{},
		ast.FuncLit{},
		ast.FuncType{},
		ast.GenDecl{},
		ast.GoStmt{},
		ast.Ident{},
		ast.IfStmt{},
		ast.ImportSpec{},
		ast.IncDecStmt{},
		ast.IndexExpr{},
		ast.IndexListExpr{},
		ast.InterfaceType{},
		ast.KeyValueExpr{},
		ast.LabeledStmt{},
		ast.MapType{},
		ast.ParenExpr{},
		ast.RangeStmt{},
		ast.ReturnStmt{},
		ast.Scope{},
		ast.SelectStmt{},
		ast.SelectorExpr{},
		ast.SendStmt{},
		ast.SliceExpr{},
		ast.StarExpr{},
		ast.StructType{},
		ast.SwitchStmt{},
		ast.TypeAssertExpr{},
		ast.TypeSpec{},
		ast.TypeSwitchStmt{},
		ast.UnaryExpr{},
		ast.ValueSpec{},
		internal.Directory{},
	}
	const filename = "encode_ast.gen.go"
	if err := codec.GenerateFile(filename, "database", types...); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wrote %s.\n", filename)
}
