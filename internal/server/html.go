// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"fmt"
	"go/doc/comment"
)

// An htmlPrinter holds the state needed for printing a Doc as HTML.
type htmlPrinter struct {
	tight bool
}

// HTML returns an HTML formatting of the Doc.
func (p *htmlPrinter) HTML(d *comment.Doc) []byte {
	var out bytes.Buffer
	for _, x := range d.Content {
		p.block(&out, x)
	}
	return out.Bytes()
}

// block prints the block x to out.
func (p *htmlPrinter) block(out *bytes.Buffer, x comment.Block) {
	switch x := x.(type) {
	default:
		fmt.Fprintf(out, "?%T", x)

	case *comment.Paragraph:
		if !p.tight {
			out.WriteString("<p>")
		}
		p.text(out, x.Text)
		out.WriteString("\n")

	case *comment.Heading:
		out.WriteString("<h4")
		if id := x.DefaultID(); id != "" {
			out.WriteString(` id="`)
			p.escape(out, id)
			out.WriteString(`"`)
		}
		out.WriteString(">")
		p.text(out, x.Text)
		out.WriteString("</h4>\n")

	case *comment.Code:
		out.WriteString("<pre>")
		p.escape(out, x.Text)
		out.WriteString("</pre>\n")

	case *comment.List:
		kind := "ol>\n"
		if x.Items[0].Number == "" {
			kind = "ul>\n"
		}
		out.WriteString("<")
		out.WriteString(kind)
		next := "1"
		for _, item := range x.Items {
			out.WriteString("<li")
			if n := item.Number; n != "" {
				if n != next {
					out.WriteString(` value="`)
					out.WriteString(n)
					out.WriteString(`"`)
					next = n
				}
				next = inc(next)
			}
			out.WriteString(">")
			p.tight = !x.BlankBetween()
			for _, blk := range item.Content {
				p.block(out, blk)
			}
			p.tight = false
		}
		out.WriteString("</")
		out.WriteString(kind)
	}
}

// inc increments the decimal string s.
// For example, inc("1199") == "1200".
func inc(s string) string {
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < '9' {
			b[i]++
			return string(b)
		}
		b[i] = '0'
	}
	return "1" + string(b)
}

// text prints the text sequence x to out.
func (p *htmlPrinter) text(out *bytes.Buffer, x []comment.Text) {
	for _, t := range x {
		switch t := t.(type) {
		case comment.Plain:
			p.escape(out, string(t))
		case comment.Italic:
			out.WriteString("<i>")
			p.escape(out, string(t))
			out.WriteString("</i>")
		case *comment.Link:
			out.WriteString(`<a href="`)
			p.escape(out, t.URL)
			out.WriteString(`">`)
			p.text(out, t.Text)
			out.WriteString("</a>")
		case *comment.DocLink:
			url := t.DefaultURL("")
			if url != "" {
				out.WriteString(`<a href="`)
				p.escape(out, url)
				out.WriteString(`">`)
			}
			p.text(out, t.Text)
			if url != "" {
				out.WriteString("</a>")
			}
		}
	}
}

// escape prints s to out as plain text,
// escaping < & " ' and > to avoid being misinterpreted
// in larger HTML constructs.
func (p *htmlPrinter) escape(out *bytes.Buffer, s string) {
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			out.WriteString(s[start:i])
			out.WriteString("&lt;")
			start = i + 1
		case '&':
			out.WriteString(s[start:i])
			out.WriteString("&amp;")
			start = i + 1
		case '"':
			out.WriteString(s[start:i])
			out.WriteString("&quot;")
			start = i + 1
		case '\'':
			out.WriteString(s[start:i])
			out.WriteString("&apos;")
			start = i + 1
		case '>':
			out.WriteString(s[start:i])
			out.WriteString("&gt;")
			start = i + 1
		}
	}
	out.WriteString(s[start:])
}
