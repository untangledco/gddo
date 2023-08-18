package render

import (
	"bytes"
	"fmt"
	"go/doc/comment"
)

// DocGemini returns a Gemini text formatting of the Doc.
func DocGemini(d *comment.Doc) string {
	p := &geminiPrinter{}
	return string(p.Gemini(d))
}

// A geminiPrinter holds the state needed for printing a Doc as Gemini text.
type geminiPrinter struct {
	tight bool
	last  bool
}

// Gemini returns a Gemini text formatting of the Doc.
func (p *geminiPrinter) Gemini(d *comment.Doc) []byte {
	var out bytes.Buffer
	for i, x := range d.Content {
		if i == len(d.Content)-1 {
			p.last = true
		}
		p.block(&out, x)
	}
	return out.Bytes()
}

// block prints the block x to out.
func (p *geminiPrinter) block(out *bytes.Buffer, x comment.Block) {
	switch x := x.(type) {
	default:
		fmt.Fprintf(out, "?%T", x)

	case *comment.Paragraph:
		p.text(out, x.Text)
		out.WriteString("\n")
		if !p.tight && !p.last {
			out.WriteString("\n")
		}

	case *comment.Heading:
		out.WriteString("### ")
		p.text(out, x.Text)
		out.WriteString("\n")
		if !p.last {
			out.WriteString("\n")
		}

	case *comment.Code:
		out.WriteString("```\n")
		out.WriteString(x.Text)
		out.WriteString("```\n")
		if !p.last {
			out.WriteString("\n")
		}

	case *comment.List:
		for _, item := range x.Items {
			if item.Number != "" {
				out.WriteString(item.Number)
				out.WriteString(". ")
			} else {
				out.WriteString("* ")
			}
			p.tight = !x.BlankBetween()
			for _, blk := range item.Content {
				p.block(out, blk)
			}
			p.tight = false
		}
	}
}

// text prints the text sequence x to out.
func (p *geminiPrinter) text(out *bytes.Buffer, x []comment.Text) {
	for _, t := range x {
		switch t := t.(type) {
		case comment.Plain:
			p.wrap(out, string(t))
		case comment.Italic:
			p.wrap(out, string(t))
		case *comment.Link:
			p.text(out, t.Text)
		case *comment.DocLink:
			p.text(out, t.Text)
		}
	}
}

// wrap prints s to out as plain text,
// replacing newlines with spaces to enable softwrapping.
func (p *geminiPrinter) wrap(out *bytes.Buffer, s string) {
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			out.WriteString(s[start:i])
			out.WriteString(" ")
			start = i + 1
		}
	}
	out.WriteString(s[start:])
}
