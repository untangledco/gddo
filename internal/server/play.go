// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/doc"
	"go/format"
	"io"
	"net/http"
	"regexp"
)

func findExamples(pkg *doc.Package, export, method string) []*doc.Example {
	if export == "package" {
		return pkg.Examples
	}
	for _, f := range pkg.Funcs {
		if f.Name == export {
			return f.Examples
		}
	}
	for _, t := range pkg.Types {
		for _, f := range t.Funcs {
			if f.Name == export {
				return f.Examples
			}
		}
		if t.Name == export {
			if method == "" {
				return t.Examples
			}
			for _, m := range t.Methods {
				if method == m.Name {
					return m.Examples
				}
			}
			return nil
		}
	}
	return nil
}

func findExample(pkg *doc.Package, export, method, name string) *doc.Example {
	for _, e := range findExamples(pkg, export, method) {
		if name == e.Name {
			return e
		}
	}
	return nil
}

var exampleIDPat = regexp.MustCompile(`([^-]+)(?:-([^-]*)(?:-(.*))?)?`)

func (s *Server) playURL(ctx context.Context, pkg *Package, id string) (string, error) {
	if m := exampleIDPat.FindStringSubmatch(id); m != nil {
		if e := findExample(pkg.Doc, m[1], m[2], m[3]); e != nil && e.Play != nil {
			var buf bytes.Buffer
			if err := format.Node(&buf, pkg.fset, e.Play); err != nil {
				return "", err
			}

			req, err := http.NewRequestWithContext(ctx, "POST", "https://play.golang.org/share", &buf)
			if err != nil {
				return "", err
			}
			req.Header.Set("Content-Type", "text/plain")
			req.Header.Set("User-Agent", s.cfg.UserAgent)
			resp, err := s.httpClient.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			p, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", err
			}
			if resp.StatusCode > 399 {
				return "", fmt.Errorf("Error from play.golang.org: %s", p)
			}
			return fmt.Sprintf("https://play.golang.org/p/%s", p), nil
		}
	}
	return "", errors.New("example not found")
}
