package markdown

import (
	"strings"
	"unicode"
)

// parseInlines cuts a text into **bold**, *italic*, `code` and [link](url)
// runs. Delimiters work as toggles, which is enough for writing a report.
func parseInlines(s string) []Inline {
	p := &inlineParser{s: s}
	p.run()
	return p.flush()
}

type inlineParser struct {
	s      string
	buf    strings.Builder
	out    []Inline
	bold   bool
	italic bool
	href   string
}

func (p *inlineParser) emit() {
	if p.buf.Len() == 0 {
		return
	}
	p.out = append(p.out, Inline{Text: p.buf.String(), Bold: p.bold, Italic: p.italic, Href: p.href})
	p.buf.Reset()
}

func (p *inlineParser) flush() []Inline {
	p.emit()
	return p.out
}

func (p *inlineParser) run() {
	s := p.s
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			p.buf.WriteByte(s[i+1])
			i += 2

		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				p.emit()
				p.out = append(p.out, Inline{Text: s[i+1 : i+1+j], Code: true, Href: p.href})
				i += j + 2
				continue
			}
			p.buf.WriteByte(s[i])
			i++

		case strings.HasPrefix(s[i:], "**"), strings.HasPrefix(s[i:], "__"):
			p.emit()
			p.bold = !p.bold
			i += 2

		case s[i] == '*' || s[i] == '_':
			if p.italic || canOpen(s, i) {
				p.emit()
				p.italic = !p.italic
				i++
				continue
			}
			p.buf.WriteByte(s[i])
			i++

		case strings.HasPrefix(s[i:], "!["):
			// Inline image: only the alt text is kept.
			if txt, _, n, ok := parseLink(s[i+1:]); ok {
				p.buf.WriteString(txt)
				i += n + 1
				continue
			}
			p.buf.WriteByte(s[i])
			i++

		case s[i] == '[':
			if txt, url, n, ok := parseLink(s[i:]); ok {
				p.emit()
				sub := &inlineParser{s: txt, bold: p.bold, italic: p.italic, href: url}
				sub.run()
				p.out = append(p.out, sub.flush()...)
				i += n
				continue
			}
			p.buf.WriteByte(s[i])
			i++

		default:
			p.buf.WriteByte(s[i])
			i++
		}
	}
}

// canOpen avoids opening emphasis on a word-internal underscore (a_variable).
func canOpen(s string, i int) bool {
	if i+1 >= len(s) || s[i+1] == ' ' {
		return false
	}
	if i > 0 && s[i] == '_' {
		prev := rune(s[i-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			return false
		}
	}
	return true
}

// parseLink reads "[text](url)" from the start of s.
func parseLink(s string) (text, url string, n int, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return "", "", 0, false
	}
	depth := 0
	end := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '[' {
			depth++
		} else if s[i] == ']' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end < 0 || end+1 >= len(s) || s[end+1] != '(' {
		return "", "", 0, false
	}
	paren := closingParen(s[end+2:])
	if paren < 0 {
		return "", "", 0, false
	}
	url = strings.TrimSpace(s[end+2 : end+2+paren])
	if j := strings.Index(url, " \""); j >= 0 {
		url = strings.TrimSpace(url[:j])
	}
	return s[1:end], url, end + 2 + paren + 1, true
}

// closingParen finds, in the text just past a destination's opening (, the
// parenthesis that closes it, letting balanced pairs through: the destination of
// [x](https://host/a_(b)) runs to the last ). It returns -1 when none closes it.
func closingParen(s string) int {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// Plain is the text of a run of inlines, formatting dropped. The table of
// contents and the tests use it.
func Plain(inls []Inline) string {
	var sb strings.Builder
	for _, in := range inls {
		sb.WriteString(in.Text)
	}
	return sb.String()
}
