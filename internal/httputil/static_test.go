// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package httputil

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
)

var (
	testHash          = computeTestHash()
	testEtag          = `"` + testHash + `"`
	testContentLength = computeTestContentLength()
)

func mustParseURL(urlStr string) *url.URL {
	u, err := url.Parse(urlStr)
	if err != nil {
		panic(err)
	}
	return u
}

func computeTestHash() string {
	p, err := os.ReadFile("static_test.go")
	if err != nil {
		panic(err)
	}
	w := sha1.New()
	w.Write(p)
	return hex.EncodeToString(w.Sum(nil))
}

func computeTestContentLength() string {
	info, err := os.Stat("static_test.go")
	if err != nil {
		panic(err)
	}
	return strconv.FormatInt(info.Size(), 10)
}

var fileServerTests = []*struct {
	name   string // test name for log
	ss     *FileServer
	r      *http.Request
	header http.Header // expected response headers
	status int         // expected response status
	empty  bool        // true if response body not expected.
}{
	{
		name: "get",
		ss:   NewFileServer(os.DirFS(".")),
		r: &http.Request{
			URL:    mustParseURL("/dir/static_test.go"),
			Method: "GET",
		},
		status: http.StatusOK,
		header: http.Header{
			"Etag":           {testEtag},
			"Cache-Control":  {"public, max-age=86400"},
			"Content-Length": {testContentLength},
		},
	},
	{
		name: "get with ?v=",
		ss:   NewFileServer(os.DirFS(".")),
		r: &http.Request{
			URL:    mustParseURL("/dir/static_test.go?v=xxxxx"),
			Method: "GET",
		},
		status: http.StatusOK,
		header: http.Header{
			"Etag":           {testEtag},
			"Cache-Control":  {"public, max-age=31536000"},
			"Content-Length": {testContentLength},
		},
	},
	{
		name: "head",
		ss:   NewFileServer(os.DirFS(".")),
		r: &http.Request{
			URL:    mustParseURL("/dir/static_test.go"),
			Method: "HEAD",
		},
		status: http.StatusOK,
		header: http.Header{
			"Etag":           {testEtag},
			"Cache-Control":  {"public, max-age=86400"},
			"Content-Length": {testContentLength},
		},
		empty: true,
	},
	{
		name: "if-none-match",
		ss:   NewFileServer(os.DirFS(".")),
		r: &http.Request{
			URL:    mustParseURL("/dir/static_test.go"),
			Method: "GET",
			Header: http.Header{"If-None-Match": {testEtag}},
		},
		status: http.StatusNotModified,
		header: http.Header{
			"Cache-Control": {"public, max-age=86400"},
			"Etag":          {testEtag},
		},
		empty: true,
	},
}

func testFileServer(t *testing.T, f func(*FileServer) http.Handler) {
	for _, tt := range fileServerTests {
		w := httptest.NewRecorder()

		h := f(tt.ss)
		h.ServeHTTP(w, tt.r)

		if w.Code != tt.status {
			t.Errorf("%s, status=%d, want %d", tt.name, w.Code, tt.status)
		}

		for k := range tt.header {
			if w.HeaderMap.Get(k) != tt.header.Get(k) {
				t.Errorf("%s\n\theader=%v,\n\twant   %v", tt.name, w.HeaderMap, tt.header)
				break
			}
		}

		empty := w.Body.Len() == 0
		if empty != tt.empty {
			t.Errorf("%s empty=%v, want %v", tt.name, empty, tt.empty)
		}
	}
}

func TestFileServer(t *testing.T) {
	testFileServer(t, func(ss *FileServer) http.Handler { return ss.FileHandler("static_test.go") })
}
