package docx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"md2report/internal/markdown"
)

// node is the span, within word/document.xml, of a child of <w:body>.
type node struct {
	name       string
	start, end int
	role       role // what becomes of it when the template is filled
}

// role says what becomes of a child of <w:body> when the template is filled.
type role int

const (
	keep       role = iota // kept as is
	coverTitle             // the cover page title
	coverDate
	coverType
	coverRef
	tocHere  // replaced by the regenerated table of contents
	bodyHere // replaced by the body built from the Markdown
)

// markerRoles maps the tag a template puts on a region to what md2report does
// with it. A template declares its regions this way and by no other means: the
// text printed around them is the template's business, and may be reworded or
// translated without md2report noticing or caring.
var markerRoles = map[string]role{
	"md2report:title":     coverTitle,
	"md2report:date":      coverDate,
	"md2report:type":      coverType,
	"md2report:reference": coverRef,
	"md2report:toc":       tocHere,
	"md2report:body":      bodyHere,
}

// tagOf names a role the way a template writes it.
func tagOf(r role) string {
	for tag, rr := range markerRoles {
		if rr == r {
			return tag
		}
	}
	return ""
}

// Default usable area, in twips, when the template declares no <w:sectPr>.
const (
	defaultContentW = 8064
	defaultContentH = 11664
)

// layout is the plan of the template. It locates, inside word/document.xml, the
// cover-page fields, the table of contents, the usable page area, and the node
// where the part of the template worth keeping ends.
//
// Walking the nodes and reading their markers both live in this file. A caller
// needs two calls: mapTemplate to draw the plan, then splice to produce the
// filled document.xml.
type layout struct {
	doc   []byte
	nodes []node

	contentStart, contentEnd int // bounds of the content of <w:body>
	sect                     int // trailing <w:sectPr>, -1 if there is none

	marks map[role]bool // the markers the template declares

	width, height int // usable page area, in twips
}

// mapTemplate establishes the plan of a word/document.xml: which region holds
// each cover field, the table of contents and the body md2report replaces. Every
// one of them is found by its marker. It fails when the reference or the body is
// not declared, there being no report to write without them.
func mapTemplate(docXML []byte) (*layout, error) {
	nodes, contentStart, contentEnd, err := bodyNodes(docXML)
	if err != nil {
		return nil, err
	}

	l := &layout{
		doc:          docXML,
		nodes:        nodes,
		contentStart: contentStart,
		contentEnd:   contentEnd,
		sect:         -1,
		width:        defaultContentW,
		height:       defaultContentH,
		marks:        map[role]bool{},
	}

	for i, n := range nodes {
		if n.name == "w:sectPr" {
			l.sect = i
		}
		if r, ok := markerRoles[markerOf(docXML[n.start:n.end])]; ok {
			l.assign(i, r)
		}
	}
	for _, r := range []role{coverRef, bodyHere} {
		if !l.marks[r] {
			return nil, fmt.Errorf("unexpected template: it declares no %s marker", tagOf(r))
		}
	}
	if l.sect >= 0 {
		l.width, l.height = pageContent(docXML[nodes[l.sect].start:nodes[l.sect].end])
	}
	return l, nil
}

// reference is the reference the template prints on its cover page. The headers
// and footers repeat it, and finding it there means knowing what it says here: a
// reference is whatever the firm numbers its reports with, and md2report copies
// it as written rather than expecting a form of its own.
func (l *layout) reference() string {
	for _, n := range l.nodes {
		if n.role == coverRef {
			return paraText(l.doc[n.start:n.end])
		}
	}
	return ""
}

// checkCover refuses a template with nowhere to put a value the front matter
// declares. A cover field a template does not offer is a fair template design; a
// field it does not offer while the front matter fills it would be dropped in
// silence, and the reader of a report cannot tell a value that was never given
// from one the template swallowed.
func (l *layout) checkCover(m markdown.Meta) error {
	for _, f := range []struct {
		key   string
		role  role
		value string
	}{
		{"title", coverTitle, m.Title},
		{"date", coverDate, m.Date},
		{"type", coverType, m.Type},
	} {
		if f.value != "" && !l.marks[f.role] {
			return fmt.Errorf("the front matter sets %q, but the template declares no %s marker to hold it", f.key, tagOf(f.role))
		}
	}
	return nil
}

// assign gives a node its role, unless it is out of the document, already spoken
// for, or a second control claiming a marker already seen: the first wins.
func (l *layout) assign(idx int, r role) {
	if idx >= 0 && idx < len(l.nodes) && l.nodes[idx].role == keep && !l.marks[r] {
		l.nodes[idx].role = r
		l.marks[r] = true
	}
}

// splice rebuilds word/document.xml: every marked region filled, and every other
// child of <w:body> copied through byte for byte, whether it comes before the
// body or after it.
func (l *layout) splice(m markdown.Meta, toc, body, contents string) []byte {
	var out strings.Builder
	out.Write(l.doc[:l.contentStart])

	for _, n := range l.nodes {
		raw := l.doc[n.start:n.end]
		switch n.role {
		case coverTitle:
			out.WriteString(coverPara(raw, m.Title))
		case coverDate:
			out.WriteString(coverPara(raw, m.Date))
		case coverType:
			out.WriteString(coverPara(raw, m.Type))
		case coverRef:
			out.WriteString(coverPara(raw, m.Reference))
		case tocHere:
			// The one control a report keeps: Word needs it to offer "update
			// the table". It keeps everything but our tag.
			out.WriteString(replaceTOC(stripMarker(raw), toc, contents))
		case bodyHere:
			out.WriteString(body)
		default:
			out.Write(raw)
		}
	}

	out.Write(l.doc[l.contentEnd:])
	return []byte(out.String())
}

// coverPara fills a cover-page field, and leaves the template's own paragraph
// alone when the front matter declares nothing. The control around it goes
// either way: a marker says what a template offers, and a report is not one.
func coverPara(raw []byte, text string) string {
	inner := unwrapSdt(raw)
	if text == "" {
		return string(inner)
	}
	return replaceParaText(inner, text)
}

var (
	reMarker    = regexp.MustCompile(`<w:tag w:val="(md2report:[a-z]+)"`)
	reMarkerTag = regexp.MustCompile(`<w:tag w:val="md2report:[a-z]+"\s*/>`)
)

// markerOf reads the marker a child of <w:body> carries, or "" for a node that
// carries none. Only the control's own properties are read: a marker further in
// belongs to a region nested inside this one, not to this node.
func markerOf(raw []byte) string {
	head := raw
	if i := bytes.Index(raw, []byte("<w:sdtContent>")); i >= 0 {
		head = raw[:i]
	}
	if m := reMarker.FindSubmatch(head); m != nil {
		return string(m[1])
	}
	return ""
}

// unwrapSdt returns what a content control holds, dropping the control itself
// and keeping its content byte for byte.
func unwrapSdt(raw []byte) []byte {
	open := bytes.Index(raw, []byte("<w:sdtContent>"))
	end := bytes.LastIndex(raw, []byte("</w:sdtContent>"))
	if open < 0 || end < open {
		return raw
	}
	return raw[open+len("<w:sdtContent>") : end]
}

// stripMarker takes md2report's tag off a control the report keeps.
func stripMarker(raw []byte) []byte { return reMarkerTag.ReplaceAll(raw, nil) }

// bodyNodes locates the direct children of <w:body> and the bounds of its
// content. It works on raw bytes so that it rewrites nothing on the way through.
func bodyNodes(doc []byte) (nodes []node, contentStart, contentEnd int, err error) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	inBody := false
	depth := 0
	curStart, curName := 0, ""

	for {
		prev := int(dec.InputOffset())
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, err
		}
		cur := int(dec.InputOffset())

		switch t := tok.(type) {
		case xml.StartElement:
			name := rawName(t.Name)
			if !inBody {
				if name == "w:body" {
					inBody, contentStart = true, cur
				}
				continue
			}
			if depth == 0 {
				curStart, curName = prev, name
			}
			depth++
		case xml.EndElement:
			if !inBody {
				continue
			}
			if depth == 0 { // </w:body>
				return nodes, contentStart, prev, nil
			}
			depth--
			if depth == 0 {
				nodes = append(nodes, node{name: curName, start: curStart, end: cur})
			}
		}
	}
	return nil, 0, 0, fmt.Errorf("no <w:body> tag found")
}

func rawName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

var (
	rePPr      = regexp.MustCompile(`(?s)<w:pPr>.*?</w:pPr>`)
	reFirstRPr = regexp.MustCompile(`(?s)<w:r(?: [^>]*)?>\s*(<w:rPr>.*?</w:rPr>)`)
	rePgSzW    = regexp.MustCompile(`<w:pgSz[^>]* w:w="(\d+)"`)
	rePgSzH    = regexp.MustCompile(`<w:pgSz[^>]* w:h="(\d+)"`)
	rePgMarL   = regexp.MustCompile(`<w:pgMar[^>]* w:left="(\d+)"`)
	rePgMarR   = regexp.MustCompile(`<w:pgMar[^>]* w:right="(\d+)"`)
	rePgMarT   = regexp.MustCompile(`<w:pgMar[^>]* w:top="(\d+)"`)
	rePgMarB   = regexp.MustCompile(`<w:pgMar[^>]* w:bottom="(\d+)"`)
)

// replaceParaText replaces every bit of text in a paragraph and keeps its
// formatting.
func replaceParaText(raw []byte, text string) string {
	pPr := ""
	if m := rePPr.Find(raw); m != nil {
		pPr = string(m)
	}
	rPr := ""
	if m := reFirstRPr.FindSubmatch(raw); m != nil {
		rPr = string(m[1])
	}
	return "<w:p>" + pPr + "<w:r>" + rPr + runText(text) + "</w:r></w:p>"
}

// replaceTOC swaps the run of TOC-styled paragraphs inside a template element
// (here the content control carrying the table of contents) for the ones just
// generated. It keeps the heading above them and the control itself.
func replaceTOC(raw []byte, entries, contents string) string {
	// The paragraphs to drop are the ones the template styled as its contents.
	tocMarker := []byte(`<w:pStyle w:val="` + contents + `"`)
	first := bytes.Index(raw, tocMarker)
	if first < 0 {
		return entries
	}
	start := paraStart(raw[:first])
	last := bytes.LastIndex(raw, tocMarker)
	rest := bytes.Index(raw[last:], []byte("</w:p>"))
	if start < 0 || rest < 0 {
		return entries
	}
	end := last + rest + len("</w:p>")
	return string(raw[:start]) + entries + string(raw[end:])
}

// paraStart returns the position of the last opening <w:p> in s.
func paraStart(s []byte) int {
	a := bytes.LastIndex(s, []byte("<w:p>"))
	b := bytes.LastIndex(s, []byte("<w:p "))
	if b > a {
		return b
	}
	return a
}

// pageContent works out the usable page width and height in twips, from the
// document's <w:sectPr>.
func pageContent(tail []byte) (int, int) {
	num := func(re *regexp.Regexp, def int) int {
		if m := re.FindSubmatch(tail); m != nil {
			if n, err := strconv.Atoi(string(m[1])); err == nil {
				return n
			}
		}
		return def
	}
	w := num(rePgSzW, 12240) - num(rePgMarL, 1440) - num(rePgMarR, 1440)
	h := num(rePgSzH, 15840) - num(rePgMarT, 1440) - num(rePgMarB, 1440)
	if w <= 0 {
		w = defaultContentW
	}
	if h <= 0 {
		h = defaultContentH
	}
	return w, h
}
