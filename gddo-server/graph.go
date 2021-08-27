// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
)

func renderGraph(pkg database.Package, pkgs []database.Package, edges [][2]int) ([]byte, error) {
	var in, out bytes.Buffer

	fmt.Fprintf(&in, "digraph %s { \n", pkg.Name)
	for i, pkg := range pkgs {
		fmt.Fprintf(&in, " n%d [label=\"%s\", URL=\"/%s\", tooltip=\"%s\"];\n",
			i, pkg.ImportPath, pkg.ImportPath,
			strings.Replace(pkg.Synopsis, `"`, `\"`, -1))
	}
	for _, edge := range edges {
		fmt.Fprintf(&in, " n%d -> n%d;\n", edge[0], edge[1])
	}
	in.WriteString("}")

	cmd := exec.Command("dot", "-Tsvg")
	cmd.Stdin = &in
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	p := out.Bytes()
	i := bytes.Index(p, []byte("<svg"))
	if i < 0 {
		return nil, errors.New("<svg not found")
	}
	p = p[i:]
	return p, nil
}
