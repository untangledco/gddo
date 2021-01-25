// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
)

// setupTestClient creates a fake module proxy for testing using the given test
// version information. If modules is nil, it will default to hosting the
// modules in the testdata directory.
//
// It returns a function for tearing down the proxy after the test is completed
// and a Client for interacting with the test proxy.
func setupTestClient(t *testing.T, modules []*Module) (*Client, func()) {
	t.Helper()
	s := NewServer(modules)
	return newClientForServer(s)
}

// newClientForServer starts serving the proxy locally. It returns a client to the
// server and a function to shut down the server.
func newClientForServer(s *Server) (*Client, func()) {
	srv := httptest.NewServer(s)
	client := &Client{
		URL: srv.URL,
	}
	return client, srv.Close
}

// zipContents creates an in-memory zip of the given contents.
func zipContents(contents map[string]string) ([]byte, error) {
	bs := &bytes.Buffer{}
	if err := writeZip(bs, contents); err != nil {
		return nil, fmt.Errorf("testhelper.ZipContents(%v): %v", contents, err)
	}
	return bs.Bytes(), nil
}

func writeZip(w io.Writer, contents map[string]string) (err error) {
	zw := zip.NewWriter(w)
	defer func() {
		if cerr := zw.Close(); cerr != nil {
			err = fmt.Errorf("error: %v, close error: %v", err, cerr)
		}
	}()

	for name, content := range contents {
		fw, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("ZipWriter::Create(): %v", err)
		}
		_, err = io.WriteString(fw, content)
		if err != nil {
			return fmt.Errorf("io.WriteString(...): %v", err)
		}
	}
	return nil
}
