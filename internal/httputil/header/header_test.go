// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package header

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var getHeaderListTests = []struct {
	s string
	l []string
}{
	{s: `a`, l: []string{`a`}},
	{s: `a, b , c `, l: []string{`a`, `b`, `c`}},
	{s: `a,, b , , c `, l: []string{`a`, `b`, `c`}},
	{s: `a,b,c`, l: []string{`a`, `b`, `c`}},
	{s: ` a b, c d `, l: []string{`a b`, `c d`}},
	{s: `"a, b, c", d `, l: []string{`"a, b, c"`, "d"}},
	{s: `","`, l: []string{`","`}},
	{s: `"\""`, l: []string{`"\""`}},
	{s: `" "`, l: []string{`" "`}},
}

func TestGetHeaderList(t *testing.T) {
	for _, tt := range getHeaderListTests {
		header := http.Header{"Foo": {tt.s}}
		if l := ParseList(header, "foo"); !cmp.Equal(tt.l, l) {
			t.Errorf("ParseList for %q = %q, want %q", tt.s, l, tt.l)
		}
	}
}
