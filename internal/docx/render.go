package docx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"md2report/internal/markdown"
)

// tocEntry is a line of the table of contents, tied to a bookmark in the body.
type tocEntry struct {
	text     string
	bookmark string
}

const (
	emuPerTwip = 635
	emuPerPx   = 9525 // at 96 dots per inch

	// Word wants every drawing id in the document unique. Templates number their
	// own drawings from 1, so ours start well clear of them.
	docPrBase = 2000
)

// docResources is everything rendering the body asks of the document. Put an
// image, a link or a numbering into it, and get back the id that refers to it.
// The .docx container stays behind this interface: the ZIP, the relationships,
// [Content_Types].xml and word/numbering.xml never show up in render.go.
//
// Two types implement it. The builder writes into the template; the fake
// registry in the tests only records what it was asked for.
type docResources interface {
	// addImage stores the bytes of an image and returns the relationship id
	// that points at it.
	addImage(data []byte, ext, mime string) (rID string)
	// addLink declares an external link and returns its relationship id. The
	// same URL always yields the same id.
	addLink(url string) (rID string)
	// addNumbering reserves a decimal numbering, restarted at 1.
	addNumbering() (numID int)
}

// renderer turns Markdown blocks into OOXML. It knows the document only through
// docResources, the template's house style and the usable page area. The
// bookmark and image counters are its own.
type renderer struct {
	res     docResources
	baseDir string // the Markdown's directory, for resolving images
	maxW    int    // usable page area, in EMU
	maxH    int

	st *styleSet // what this template says about house style

	nextImage    int // 1 for the first image; docPrBase offsets the OOXML ids
	nextBookmark int
}

// newRenderer prepares a rendering into a page whose usable area is given in
// twips.
func newRenderer(res docResources, st *styleSet, baseDir string, twipsW, twipsH int) *renderer {
	return &renderer{
		res:          res,
		st:           st,
		baseDir:      baseDir,
		maxW:         twipsW * emuPerTwip,
		maxH:         twipsH * emuPerTwip,
		nextBookmark: 1000,
	}
}

// body produces the document body and the list of table-of-contents entries.
func (r *renderer) body(blocks []markdown.Block) (string, []tocEntry, error) {
	var out strings.Builder
	var toc []tocEntry
	figure := 0
	numIDs := map[int]int{}

	for _, blk := range blocks {
		switch b := blk.(type) {
		case markdown.Heading:
			if b.Level == 1 {
				r.nextBookmark++
				bm := "_TocMd" + strconv.Itoa(r.nextBookmark)
				toc = append(toc, tocEntry{text: markdown.Plain(b.Inlines), bookmark: bm})
				fmt.Fprintf(&out, `<w:p><w:pPr><w:pStyle w:val="%s"/></w:pPr><w:bookmarkStart w:id="%d" w:name="%s"/>%s<w:bookmarkEnd w:id="%d"/></w:p>`,
					r.st.get(styPart), r.nextBookmark, bm, r.runs(b.Inlines), r.nextBookmark)
				continue
			}
			out.WriteString(para(r.st.get(styHeading(b.Level)), "", r.runs(b.Inlines)))

		case markdown.Paragraph:
			out.WriteString(para(r.st.get(styBody), "", r.runs(b.Inlines)))

		case markdown.Bullet:
			bullet := r.st.get(styBullet)
			out.WriteString(para(bullet, listPPr(b.Level, r.st.bulletNum), r.runs(b.Inlines)))

		case markdown.Ordered:
			id, ok := numIDs[b.ListID]
			if !ok {
				id = r.res.addNumbering()
				numIDs[b.ListID] = id
			}
			out.WriteString(para(r.st.get(styBullet), listPPr(b.Level, id), r.runs(b.Inlines)))

		case markdown.Quote:
			out.WriteString(para(r.st.get(styQuote), "", r.runs(b.Inlines)))

		case markdown.Code:
			rPr := `<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="18"/></w:rPr>`
			extra := `<w:jc w:val="left"/><w:spacing w:before="120" w:after="120" w:line="240" w:lineRule="auto"/>`
			out.WriteString(para(r.st.get(styBody), extra, "<w:r>"+rPr+runText(b.Text)+"</w:r>"))

		case markdown.Image:
			drawing, err := r.image(b.Path, b.Width)
			if err != nil {
				return "", nil, err
			}
			out.WriteString(para(r.st.get(styImage), "", drawing))
			if len(b.Caption) > 0 {
				figure++
				legend := []markdown.Inline{{Text: strings.ReplaceAll(r.st.caption, "{n}", strconv.Itoa(figure))}}
				out.WriteString(para(r.st.get(styFigureCaption), "", r.runs(append(legend, b.Caption...))))
			}

		case markdown.Table:
			out.WriteString(r.table(b))
		}
	}
	return out.String(), toc, nil
}

func para(style, extraPPr, content string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/>` + extraPPr + `</w:pPr>` + content + `</w:p>`
}

// listPPr forces the level and numbering of a list item. Only nested levels need
// an indent, since the style already carries the one for level 0.
func listPPr(level, numID int) string {
	var pPr string
	if numID > 0 {
		// With no numbering to name, the style is left to supply its own.
		pPr = fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, level, numID)
	}
	if level > 0 {
		pPr += fmt.Sprintf(`<w:ind w:left="%d" w:hanging="357"/>`, 714+level*357)
	}
	return pPr
}

// runs turns Markdown fragments into Word runs.
func (r *renderer) runs(inls []markdown.Inline) string {
	var sb strings.Builder
	for _, in := range inls {
		if in.Text == "" {
			continue
		}
		// A destination Word must not be handed keeps its text and loses only
		// the click.
		href := in.Href
		if !linkable(href) {
			href = ""
		}
		var rPr strings.Builder
		if href != "" {
			fmt.Fprintf(&rPr, `<w:rStyle w:val="%s"/>`, r.st.get(styLink))
		}
		if in.Code {
			rPr.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
		}
		if in.Bold {
			rPr.WriteString(`<w:b/><w:bCs/>`)
		}
		if in.Italic {
			rPr.WriteString(`<w:i/><w:iCs/>`)
		}
		run := "<w:r>"
		if rPr.Len() > 0 {
			run += "<w:rPr>" + rPr.String() + "</w:rPr>"
		}
		run += runText(in.Text) + "</w:r>"

		if href != "" {
			run = fmt.Sprintf(`<w:hyperlink r:id="%s" w:history="1">%s</w:hyperlink>`, r.res.addLink(href), run)
		}
		sb.WriteString(run)
	}
	return sb.String()
}

// runText encodes the textual content of a run, tabs and line breaks included.
func runText(s string) string {
	if s == "" {
		return `<w:t xml:space="preserve"></w:t>`
	}
	var sb strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			sb.WriteString("<w:br/>")
		}
		for j, seg := range strings.Split(line, "\t") {
			if j > 0 {
				sb.WriteString("<w:tab/>")
			}
			if seg != "" {
				sb.WriteString(`<w:t xml:space="preserve">` + esc(seg) + `</w:t>`)
			}
		}
	}
	return sb.String()
}

// linkable reports whether a destination may become a clickable relationship.
//
// A report quotes the addresses of an incident at least as often as it means to
// follow them, and Word goes wherever the target names: a \\host\share or a
// file:// hands the reader's machine, and its credentials, to whatever wrote the
// address. Only the three schemes a report has any business linking to are let
// through; anything else stays as text, reading exactly as it was written.
func linkable(href string) bool {
	for _, scheme := range []string{"http://", "https://", "mailto:"} {
		if len(href) >= len(scheme) && strings.EqualFold(href[:len(scheme)], scheme) {
			return true
		}
	}
	return false
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// esc prepares a text for XML content: the markup characters escaped, and the
// characters XML cannot hold dropped. A report quotes what the incident left
// behind, and a terminal capture pasted into it carries the ANSI escapes and
// stray control bytes that came with it. There is no escaping those, so a run
// left holding one makes the whole document.xml ill-formed and Word refuses to
// open the file it is offered.
func esc(s string) string { return escaper.Replace(stripInvalidXML(s)) }

// stripInvalidXML drops what XML 1.0 admits nowhere: the C0 controls other than
// tab, newline and carriage return, the surrogates, and the two non-characters
// closing the basic plane. Bytes that are not UTF-8 go the same way, since a
// .docx is UTF-8 throughout. A text with nothing to drop is returned as it is.
func stripInvalidXML(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, invalidXML) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if invalidXML(r) {
			return -1
		}
		return r
	}, s)
}

func invalidXML(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return false
	case r < 0x20:
		return true
	case r >= 0xD800 && r <= 0xDFFF:
		return true
	case r == 0xFFFE || r == 0xFFFF:
		return true
	}
	return false
}

// emuPerUnit gives, for each unit the Markdown may ask a width in, its length
// in English Metric Units, the unit Word stores drawings in.
var emuPerUnit = map[string]int{"px": emuPerPx, "pt": 12700, "cm": 360000, "mm": 36000, "in": 914400}

// targetWidth converts the width asked for into EMU, against the usable page
// width. It returns 0 when the Markdown asked for none.
func targetWidth(w markdown.ImageWidth, maxW int) int {
	emu := w.Value * float64(emuPerUnit[w.Unit])
	if w.Unit == "%" {
		emu = float64(maxW) * w.Value / 100
	}
	// The conversion is capped at the page rather than left to overflow. A width
	// the Markdown asks in the millions of inches does not fit an int once it is
	// in EMU, and what comes out the other side is a drawing sized zero, which
	// Word shows as nothing at all. Wider than the page is brought back to the
	// page either way, so nothing legitimate is lost.
	if emu > float64(maxW) {
		return maxW
	}
	return int(emu)
}

// image puts an image file into the document and returns the matching <w:drawing>
// run, at the width asked for, never wider than the page.
func (r *renderer) image(path string, want markdown.ImageWidth) (string, error) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(r.baseDir, path)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("image %s: %w", path, err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("image %s: unrecognised format (png, jpeg and gif are accepted)", path)
	}

	ext, mime := format, "image/"+format
	if format == "jpeg" {
		ext = "jpg"
	}
	rID := r.res.addImage(data, ext, mime)

	r.nextImage++
	docPr := docPrBase + r.nextImage
	cx, cy := cfg.Width*emuPerPx, cfg.Height*emuPerPx
	if target := targetWidth(want, r.maxW); target > 0 && cx > 0 {
		cy, cx = cy*target/cx, target
	}
	cx, cy = fit(cx, cy, r.maxW, r.maxH)
	descr := esc(filepath.Base(path))

	return fmt.Sprintf(`<w:r><w:rPr><w:noProof/></w:rPr><w:drawing>`+
		`<wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/>`+
		`<wp:docPr id="%d" name="Image %d" descr="%s"/>`+
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" noChangeAspect="1"/></wp:cNvGraphicFramePr>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="Image %d" descr="%s"/><pic:cNvPicPr><a:picLocks noChangeAspect="1" noChangeArrowheads="1"/></pic:cNvPicPr></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s" cstate="print"/><a:srcRect/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr bwMode="auto"><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln><a:noFill/></a:ln></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, docPr, r.nextImage, descr, docPr, r.nextImage, descr, rID, cx, cy), nil
}

// fit shrinks an image to the usable page area at constant proportions. An image
// that already fits keeps its native size.
func fit(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxW, maxW / 2
	}
	if w > maxW {
		h = h * maxW / w
		w = maxW
	}
	if h > maxH {
		w = w * maxH / h
		h = maxH
	}
	return w, h
}

func (r *renderer) table(blk markdown.Table) string {
	cols := 0
	for _, row := range blk.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return ""
	}
	twips := r.maxW / emuPerTwip
	width := twips / cols

	var sb strings.Builder
	fmt.Fprintf(&sb, `<w:tbl><w:tblPr><w:tblStyle w:val="%s"/>`, r.st.get(styTable))
	fmt.Fprintf(&sb, `<w:tblW w:w="%d" w:type="dxa"/>`, width*cols)
	sb.WriteString(`<w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		fmt.Fprintf(&sb, `<w:%s w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/>`, side)
	}
	sb.WriteString(`</w:tblBorders><w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		fmt.Fprintf(&sb, `<w:gridCol w:w="%d"/>`, width)
	}
	sb.WriteString(`</w:tblGrid>`)

	for i, row := range blk.Rows {
		header := i == 0
		sb.WriteString(`<w:tr>`)
		if header {
			sb.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for c := 0; c < cols; c++ {
			var inls []markdown.Inline
			if c < len(row) {
				inls = row[c]
			}
			if header {
				bold := make([]markdown.Inline, len(inls))
				copy(bold, inls)
				for j := range bold {
					bold[j].Bold = true
				}
				inls = bold
			}
			fmt.Fprintf(&sb, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, width)
			if header {
				sb.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`)
			}
			sb.WriteString(`</w:tcPr>`)
			sb.WriteString(para(r.st.get(styBody), `<w:spacing w:before="60" w:after="60"/><w:jc w:val="left"/>`, r.runs(inls)))
			sb.WriteString(`</w:tc>`)
		}
		sb.WriteString(`</w:tr>`)
	}
	sb.WriteString(`</w:tbl>`)
	// An empty paragraph keeps a following table from merging into this one.
	sb.WriteString(para(r.st.get(styBody), `<w:spacing w:before="0" w:after="0"/>`, ""))
	return sb.String()
}

// tocField rebuilds the template's TOC field from the Markdown headings. Each
// page number is a PAGEREF field that Word recomputes when the document opens.
func tocField(entries []tocEntry, st *styleSet) string {
	style := `<w:pPr><w:pStyle w:val="` + st.get(styContents) + `"/></w:pPr>`
	open := `<w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> TOC \o "1-1" \h \z \u </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>`
	closeField := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`

	if len(entries) == 0 {
		return `<w:p>` + style + open + closeField + `</w:p>`
	}

	var sb strings.Builder
	for i, e := range entries {
		sb.WriteString(`<w:p>` + style)
		if i == 0 {
			sb.WriteString(open)
		}
		sb.WriteString(tocEntryXML(e, st))
		if i == len(entries)-1 {
			sb.WriteString(closeField)
		}
		sb.WriteString(`</w:p>`)
	}
	return sb.String()
}

func tocEntryXML(e tocEntry, st *styleSet) string {
	const hidden = `<w:rPr><w:webHidden/></w:rPr>`
	return fmt.Sprintf(`<w:hyperlink w:anchor="%s" w:history="1">`+
		`<w:r><w:rPr><w:rStyle w:val="%s"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r>`+
		`<w:r>%s<w:tab/></w:r>`+
		`<w:r>%s<w:fldChar w:fldCharType="begin"/></w:r>`+
		`<w:r>%s<w:instrText xml:space="preserve"> PAGEREF %s \h </w:instrText></w:r>`+
		`<w:r>%s<w:fldChar w:fldCharType="separate"/></w:r>`+
		`<w:r>%s<w:t>1</w:t></w:r>`+
		`<w:r>%s<w:fldChar w:fldCharType="end"/></w:r>`+
		`</w:hyperlink>`,
		e.bookmark, st.get(styHyperlink), esc(e.text), hidden, hidden, hidden, e.bookmark, hidden, hidden, hidden)
}
