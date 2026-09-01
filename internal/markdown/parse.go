package markdown

import (
	"strconv"
	"strings"
	"unicode"
)

// Parse reads a Markdown document: front matter, the body cut into blocks, then
// variable expansion into the text those blocks hold.
func Parse(src string) *Document {
	meta, body, res := parseFrontMatter(src)
	blocks := parseBlocks(body)
	expandBlocks(blocks, res)
	return &Document{Meta: meta, Blocks: blocks, Unknown: res.unresolved()}
}

// expandBlocks replaces {{variables}} everywhere text can stand in a parsed
// document: the runs of every block, the text and destination of a link, an
// image's path and caption, the lines of a code block, every cell of a table.
// Expanding here rather than over the source is what keeps a front-matter value
// beginning with # or - from opening a block of its own.
//
// What it could not resolve stays written as it was, and the resolver remembers
// it: the caller asks that of the resolver rather than of this function, which
// is also how a name left unresolved in the front matter is reported whether or
// not the body ever mentions it.
func expandBlocks(blocks []Block, res *resolver) {
	runs := func(ins []Inline) []Inline {
		for i := range ins {
			ins[i].Text = res.expand(ins[i].Text)
			ins[i].Href = res.expand(ins[i].Href)
		}
		return ins
	}

	for i, b := range blocks {
		switch v := b.(type) {
		case Heading:
			v.Inlines = runs(v.Inlines)
			blocks[i] = v
		case Paragraph:
			v.Inlines = runs(v.Inlines)
			blocks[i] = v
		case Bullet:
			v.Inlines = runs(v.Inlines)
			blocks[i] = v
		case Ordered:
			v.Inlines = runs(v.Inlines)
			blocks[i] = v
		case Quote:
			v.Inlines = runs(v.Inlines)
			blocks[i] = v
		case Code:
			v.Text = res.expand(v.Text)
			blocks[i] = v
		case Image:
			v.Path = res.expand(v.Path)
			v.Caption = runs(v.Caption)
			blocks[i] = v
		case Table:
			for r := range v.Rows {
				for c := range v.Rows[r] {
					v.Rows[r][c] = runs(v.Rows[r][c])
				}
			}
			blocks[i] = v
		}
	}
}

// parseBlocks cuts a document body into top-level blocks. The caller has already
// detached the front matter; the variables are still written as they were.
func parseBlocks(src string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var blocks []Block
	orderedID := 0
	prevOrdered := false
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			// A blank line between two items does not end the list; it only
			// spaces it out. What ends a list is another kind of block.
			i++

		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence := trimmed[:3]
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				code = append(code, lines[i])
				i++
			}
			i++ // closing fence
			blocks = append(blocks, Code{Text: strings.Join(code, "\n")})
			prevOrdered = false

		case isHR(trimmed):
			prevOrdered = false
			i++

		case headingLevel(trimmed) > 0:
			n := headingLevel(trimmed)
			blocks = append(blocks, Heading{
				Level:   n,
				Inlines: parseInlines(strings.TrimSpace(trimmed[n:])),
			})
			prevOrdered = false
			i++

		default:
			// Image alone on its line: ![caption](path)
			if path, caption, width, ok := parseImageLine(trimmed); ok {
				blocks = append(blocks, Image{
					Path:    path,
					Caption: parseInlines(caption),
					Width:   width,
				})
				prevOrdered = false
				i++
				continue
			}

			// Quotation
			if isQuote(trimmed) {
				var parts []string
				for i < len(lines) {
					t := strings.TrimSpace(lines[i])
					if !strings.HasPrefix(t, ">") {
						break
					}
					parts = append(parts, strings.TrimSpace(strings.TrimPrefix(t, ">")))
					i++
				}
				blocks = append(blocks, Quote{Inlines: parseInlines(strings.Join(parts, " "))})
				prevOrdered = false
				continue
			}

			// List item, with any continuation lines
			if indent, ordered, rest, ok := parseListItem(line); ok {
				level := indent / 2
				if level > 4 {
					level = 4
				}
				parts := []string{rest}
				i++
				for i < len(lines) && !isBlockStart(lines, i) {
					parts = append(parts, strings.TrimSpace(lines[i]))
					i++
				}
				inlines := parseInlines(strings.Join(parts, " "))
				if ordered {
					if !prevOrdered {
						orderedID++
					}
					blocks = append(blocks, Ordered{Level: level, ListID: orderedID, Inlines: inlines})
					prevOrdered = true
				} else {
					blocks = append(blocks, Bullet{Level: level, Inlines: inlines})
					prevOrdered = false
				}
				continue
			}

			// Table: a row of cells followed by a separator line
			if isTableStart(lines, i) {
				tbl := Table{Rows: [][][]Inline{parseRow(trimmed)}}
				i += 2
				for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
					tbl.Rows = append(tbl.Rows, parseRow(strings.TrimSpace(lines[i])))
					i++
				}
				blocks = append(blocks, tbl)
				prevOrdered = false
				continue
			}

			// Paragraph: the non-empty lines that follow are joined onto it.
			parts := []string{trimmed}
			i++
			for i < len(lines) && !isBlockStart(lines, i) {
				parts = append(parts, strings.TrimSpace(lines[i]))
				i++
			}
			if trimmed != "" {
				blocks = append(blocks, Paragraph{Inlines: parseInlines(strings.Join(parts, " "))})
			}
			prevOrdered = false
		}
	}
	return blocks
}

// isBlockStart reports whether the line opens a new block, and therefore ends
// the paragraph or list item in progress.
func isBlockStart(lines []string, i int) bool {
	line := lines[i]
	t := strings.TrimSpace(line)
	switch {
	case t == "", isHR(t):
		return true
	case headingLevel(t) > 0, isQuote(t),
		strings.HasPrefix(t, "```"), strings.HasPrefix(t, "~~~"):
		return true
	}
	if _, _, _, ok := parseListItem(line); ok {
		return true
	}
	if isTableStart(lines, i) {
		return true
	}
	_, _, _, ok := parseImageLine(t)
	return ok
}

// isTableStart reports whether a table opens at line i: a row of cells with a
// separator under it. It takes the whole slice because that second line is what
// tells a table apart from a paragraph that happens to contain a pipe, and both
// the parser and isBlockStart have to agree on the answer.
func isTableStart(lines []string, i int) bool {
	return strings.Contains(lines[i], "|") && i+1 < len(lines) && isTableSep(lines[i+1])
}

// headingLevel reports the level of a heading, or 0 for a line that merely
// begins with a hash. A wrapped line reading "#1 priority" carries no marker and
// belongs to the paragraph above it, which is a judgement parseBlocks and
// isBlockStart have to make the same way.
func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(s) || s[n] != ' ' {
		return 0
	}
	return n
}

// isQuote reports whether a line opens a quotation. A line beginning ">=" is a
// comparison, not a quotation.
func isQuote(s string) bool { return strings.HasPrefix(s, "> ") || s == ">" }

func isHR(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	return strings.Trim(s, string(c)+" ") == ""
}

func isTableSep(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "-") {
		return false
	}
	return strings.Trim(s, "|:- \t") == ""
}

func parseRow(s string) [][]Inline {
	s = strings.TrimPrefix(strings.TrimSpace(s), "|")
	s = strings.TrimSuffix(s, "|")
	var row [][]Inline
	for _, c := range strings.Split(s, "|") {
		row = append(row, parseInlines(strings.TrimSpace(c)))
	}
	return row
}

// parseListItem recognises "- text", "* text", "+ text" and "1. text".
//
// The indent it reports is a width, counting a tab as two spaces, and is what
// the caller turns into a nesting level. Where the marker starts is a separate
// question: a tab is two wide but one byte long, so the two are counted apart
// rather than one standing in for the other.
func parseListItem(line string) (indent int, ordered bool, rest string, ok bool) {
	off := 0
	for off < len(line) && (line[off] == ' ' || line[off] == '\t') {
		if line[off] == '\t' {
			indent += 2
		} else {
			indent++
		}
		off++
	}
	s := line[off:]
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ' {
		return indent, false, strings.TrimSpace(s[2:]), true
	}
	n := 0
	for n < len(s) && unicode.IsDigit(rune(s[n])) {
		n++
	}
	if n > 0 && n+1 < len(s) && (s[n] == '.' || s[n] == ')') && s[n+1] == ' ' {
		return indent, true, strings.TrimSpace(s[n+2:]), true
	}
	return 0, false, "", false
}

// parseImageLine recognises a line holding nothing but the image
// ![caption](path), optionally followed by {width=…} attributes.
func parseImageLine(s string) (path, caption string, width ImageWidth, ok bool) {
	if !strings.HasPrefix(s, "![") {
		return "", "", width, false
	}
	end := strings.Index(s, "](")
	if end < 0 {
		return "", "", width, false
	}
	caption = s[2:end]
	rest := s[end+2:]
	paren := closingParen(rest)
	if paren < 0 {
		return "", "", width, false
	}
	path = strings.TrimSpace(rest[:paren])
	// Optional title: ![caption](path "title")
	if j := strings.Index(path, " \""); j >= 0 {
		path = strings.TrimSpace(path[:j])
	}
	if path == "" || strings.Contains(path, "](") {
		return "", "", width, false
	}
	attrs := strings.TrimSpace(rest[paren+1:])
	if attrs != "" {
		if !strings.HasPrefix(attrs, "{") || !strings.HasSuffix(attrs, "}") {
			return "", "", width, false // text follows: not an image on its own
		}
		width = parseImageAttrs(attrs[1 : len(attrs)-1])
	}
	return path, caption, width, true
}

// parseImageAttrs reads a Pandoc-style attribute block: {width=60%},
// {width=8cm}, {width=480px}. It recognises the units %, px, pt, cm, mm and in,
// and reads a bare value as pixels.
func parseImageAttrs(attrs string) ImageWidth {
	var w ImageWidth
	for _, field := range strings.FieldsFunc(attrs, func(r rune) bool { return r == ' ' || r == ',' || r == ';' }) {
		key, val, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		if strings.ToLower(strings.TrimSpace(key)) != "width" {
			continue
		}
		val = strings.ToLower(unquote(strings.TrimSpace(val)))
		unit := ""
		for _, u := range []string{"%", "px", "pt", "cm", "mm", "in"} {
			if strings.HasSuffix(val, u) {
				unit = u
				break
			}
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(val, unit)), 64)
		if err != nil || n <= 0 {
			continue
		}
		if unit == "" {
			unit = "px" // a bare value is pixels
		}
		w = ImageWidth{Value: n, Unit: unit}
	}
	return w
}
