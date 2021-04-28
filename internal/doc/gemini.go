// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Godoc comment to Gemini formatting.
// Adapted from go/doc.

package doc

import (
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ulquo = "“"
	urquo = "”"
)

var unicodeQuoteReplacer = strings.NewReplacer("``", ulquo, "''", urquo)

func convertQuotes(text string) string {
	return unicodeQuoteReplacer.Replace(text)
}

func indentLen(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func isBlank(s string) bool {
	return len(s) == 0 || (len(s) == 1 && s[0] == '\n')
}

func commonPrefix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[0:i]
}

func unindent(block []string) {
	if len(block) == 0 {
		return
	}

	// compute maximum common white prefix
	prefix := block[0][0:indentLen(block[0])]
	for _, line := range block {
		if !isBlank(line) {
			prefix = commonPrefix(prefix, line[0:indentLen(line)])
		}
	}
	n := len(prefix)

	// remove
	for i, line := range block {
		if !isBlank(line) {
			block[i] = line[n:]
		}
	}
}

// heading returns the trimmed line if it passes as a section heading;
// otherwise it returns the empty string.
func heading(line string) string {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return ""
	}

	// a heading must start with an uppercase letter
	r, _ := utf8.DecodeRuneInString(line)
	if !unicode.IsLetter(r) || !unicode.IsUpper(r) {
		return ""
	}

	// it must end in a letter or digit:
	r, _ = utf8.DecodeLastRuneInString(line)
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return ""
	}

	// exclude lines with illegal characters. we allow "(),"
	if strings.ContainsAny(line, ";:!?+*/=[]{}_^°&§~%#@<\">\\") {
		return ""
	}

	// allow "'" for possessive "'s" only
	for b := line; ; {
		i := strings.IndexRune(b, '\'')
		if i < 0 {
			break
		}
		if i+1 >= len(b) || b[i+1] != 's' || (i+2 < len(b) && b[i+2] != ' ') {
			return "" // not followed by "s "
		}
		b = b[i+2:]
	}

	// allow "." when followed by non-space
	for b := line; ; {
		i := strings.IndexRune(b, '.')
		if i < 0 {
			break
		}
		if i+1 >= len(b) || b[i+1] == ' ' {
			return "" // not followed by non-space
		}
		b = b[i+1:]
	}

	return line
}

type op int

const (
	opPara op = iota
	opHead
	opPre
)

type block struct {
	op    op
	lines []string
}

func blocks(text string) []block {
	var (
		out  []block
		para []string

		lastWasBlank   = false
		lastWasHeading = false
	)

	close := func() {
		if para != nil {
			out = append(out, block{opPara, para})
			para = nil
		}
	}

	lines := strings.SplitAfter(text, "\n")
	unindent(lines)
	for i := 0; i < len(lines); {
		line := lines[i]
		if isBlank(line) {
			// close paragraph
			close()
			i++
			lastWasBlank = true
			continue
		}
		if indentLen(line) > 0 {
			// close paragraph
			close()

			// count indented or blank lines
			j := i + 1
			for j < len(lines) && (isBlank(lines[j]) || indentLen(lines[j]) > 0) {
				j++
			}
			// but not trailing blank lines
			for j > i && isBlank(lines[j-1]) {
				j--
			}
			pre := lines[i:j]
			i = j

			unindent(pre)

			// put those lines in a pre block
			out = append(out, block{opPre, pre})
			lastWasHeading = false
			continue
		}

		if lastWasBlank && !lastWasHeading && i+2 < len(lines) &&
			isBlank(lines[i+1]) && !isBlank(lines[i+2]) && indentLen(lines[i+2]) == 0 {
			// current line is non-blank, surrounded by blank lines
			// and the next non-blank line is not indented: this
			// might be a heading.
			if head := heading(line); head != "" {
				close()
				out = append(out, block{opHead, []string{head}})
				i += 2
				lastWasHeading = true
				continue
			}
		}

		// open paragraph
		lastWasBlank = false
		lastWasHeading = false
		para = append(para, lines[i])
		i++
	}
	close()

	return out
}

// ToGemini prepares comment text for presentation in Gemini output.
// Paragraphs of text are output on a single line with no wrapping.
// Preformatted sections are surrounded by preformatting toggle lines (```).
//
// A pair of (consecutive) backticks (`) is converted to a unicode left quote (“),
// and a pair of (consecutive) single quotes (') is converted to a unicode right quote (”).
func ToGemini(w io.Writer, text string) {
	l := lineWriter{
		out: w,
	}
	for _, b := range blocks(text) {
		switch b.op {
		case opPara:
			// l.write will add leading newline if required
			for _, line := range b.lines {
				line = convertQuotes(line)
				l.write(line)
			}
			l.flush()
		case opHead:
			w.Write(nl)
			for _, line := range b.lines {
				line = convertQuotes(line)
				l.write(line + "\n")
			}
			l.flush()
		case opPre:
			w.Write(nl)
			w.Write([]byte("```\n"))
			for _, line := range b.lines {
				if isBlank(line) {
					w.Write([]byte("\n"))
				} else {
					w.Write([]byte(line))
				}
			}
			w.Write([]byte("```\n"))
		}
	}
}

type lineWriter struct {
	out       io.Writer
	printed   bool
	n         int
	pendSpace int
}

var nl = []byte("\n")
var space = []byte(" ")

func (l *lineWriter) write(text string) {
	if l.n == 0 && l.printed {
		l.out.Write(nl) // blank line before new paragraph
	}
	l.printed = true

	for _, f := range strings.Fields(text) {
		w := utf8.RuneCountInString(f)
		l.out.Write(space[:l.pendSpace])
		l.out.Write([]byte(f))
		l.n += l.pendSpace + w
		l.pendSpace = 1
	}
}

func (l *lineWriter) flush() {
	if l.n == 0 {
		return
	}
	l.out.Write(nl)
	l.pendSpace = 0
	l.n = 0
}
