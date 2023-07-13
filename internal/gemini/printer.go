package gemini

import (
	"bytes"
	"go/doc/comment"
	"strings"
)

// Print prints a doc comment as Gemini text.
func Print(d *comment.Doc) []byte {
	var buf bytes.Buffer
	for _, block := range d.Content {
		writeBlock(&buf, block)
		buf.WriteByte('\n')
	}
	for _, linkDef := range d.Links {
		if !linkDef.Used {
			continue
		}
	}
	return buf.Bytes()
}

func writeBlock(buf *bytes.Buffer, block comment.Block) {
	switch block := block.(type) {
	case *comment.Code:
		buf.WriteString("```\n")
		buf.WriteString(block.Text)
		buf.WriteString("```\n")

	case *comment.Heading:
		buf.WriteByte('\n')
		writeText(buf, block.Text)
		buf.WriteByte('\n')

	case *comment.List:
		for _, item := range block.Items {
			if item.Number != "" {
				buf.WriteString(item.Number)
				buf.WriteByte(' ')
			} else {
				buf.WriteString("* ")
			}
			for _, block := range item.Content {
				// Currently, this can only be a Paragraph
				writeBlock(buf, block)
			}
		}

	case *comment.Paragraph:
		writeText(buf, block.Text)
		buf.WriteByte('\n')
	}
}

func writeText(buf *bytes.Buffer, text []comment.Text) {
	for _, text := range text {
		switch text := text.(type) {
		case comment.Plain:
			s := strings.ReplaceAll(string(text), "\n", " ")
			buf.WriteString(s)
		case comment.Italic:
			// We don't italicize any words
			buf.WriteString(string(text))
		case *comment.Link:
			// Links are currently unsupported
			writeText(buf, text.Text)
		case *comment.DocLink:
			// DocLinks are currently unsupported
			writeText(buf, text.Text)
		}
	}
}
