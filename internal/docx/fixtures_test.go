package docx

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// A minimal template, cut down to the regions md2report fills: a marker around
// each cover value, one around the table of contents and one around the body.
// The labels printed above the cover values are in the template's own language,
// which md2report never reads: they are here to prove it does not. The
// paragraph after the body region is there to prove a template may carry
// content below the report. Keeping it in a string means the tests can check
// the plan and the filling without a binary .docx.
func marked(tag, content string) string {
	return `<w:sdt><w:sdtPr><w:tag w:val="md2report:` + tag + `"/><w:id w:val="1"/></w:sdtPr>` +
		`<w:sdtContent>` + content + `</w:sdtContent></w:sdt>`
}

func styled(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

var templateDocumentXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"` +
	` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
	` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><w:body>` +
	marked("title", `<w:p><w:pPr><w:pStyle w:val="Titre"/></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t>Titre du modèle</w:t></w:r></w:p>`) +
	styled("Body", "Date de parution") +
	marked("date", styled("Body", "Vendredi 1 janvier 2100")) +
	styled("Body", "Type d'incident") +
	marked("type", styled("Body", "Hameçonnage")) +
	styled("Body", "Référence") +
	marked("reference", styled("Body", "MODELE-000")) +
	marked("toc", styled("Contents", "Ancienne entrée")) +
	marked("body", styled("Body", "Corps du modèle")) +
	styled("Body", "Mentions légales") +
	`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417"/></w:sectPr>` +
	`</w:body></w:document>`

// templateStylesXML defines every style a report can ask for, spelled the way
// md2report names it, so that a test filling this template is refused no style
// it did not go looking for. Bullet carries no numbering of its own, as the
// tests covering numbering assume. Titre is the template's own cover style,
// which md2report never writes and never resolves.
var templateStylesXML = func() string {
	s := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`
	for _, st := range []struct{ id, typ string }{
		{"Titre", "paragraph"},
		{styPart, "paragraph"}, {styBody, "paragraph"}, {styBullet, "paragraph"},
		{styQuote, "paragraph"}, {styImage, "paragraph"}, {styFigureCaption, "paragraph"},
		{styContents, "paragraph"},
		{styLink, "character"}, {styHyperlink, "character"},
		{styTable, "table"},
	} {
		s += `<w:style w:type="` + st.typ + `" w:styleId="` + st.id + `"><w:name w:val="` + st.id + `"/></w:style>`
	}
	for level := 2; level <= 6; level++ {
		s += `<w:style w:type="paragraph" w:styleId="` + styHeading(level) + `"><w:name w:val="` + styHeading(level) + `"/></w:style>`
	}
	return s + `</w:styles>`
}()

// templateStyles resolves the fixture's style sheet, for the tests that render
// without going through a whole template.
func templateStyles() *styleSet { return resolveStyles([]byte(templateStylesXML), nil) }

// templateFooterXML is the page footer, carrying the reference in a text box.
// Word writes such a box twice, once per drawing dialect, and the sentence below
// cites the same incident: the reference must be rewritten in both boxes and
// left alone in the sentence.
//
// The second box holds the reference in two runs, which is what Word leaves
// behind once somebody has edited a placeholder in the middle. The reference
// itself carries no prefix, since a template numbers its reports however the
// firm does and md2report reads the form off the cover page rather than
// expecting one.
const templateFooterXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:p><w:r><w:t>MODELE-000</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t>MODELE</w:t></w:r><w:r><w:t>-000</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t>Fait suite à l'incident MODELE-000, classé sans suite.</w:t></w:r></w:p>` +
	`</w:ftr>`

// minimalTemplate writes a complete .docx to disk, cut down to the parts the tool
// reads or rewrites.
func minimalTemplate(t *testing.T) string { return minimalTemplateWith(t, nil) }

// minimalTemplateWith writes the same template with some parts replaced, for the
// tests that need a template spelling something its own way.
func minimalTemplateWith(t *testing.T, overrides map[string]string) string {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
			`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`,
		"word/document.xml": templateDocumentXML,
		"word/styles.xml":   templateStylesXML,
		// Properties as Word leaves them: the template's own revision count, its
		// authoring time and the length of its body, all describing the template.
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
			` xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"` +
			` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
			`<dc:title></dc:title><dc:subject></dc:subject><dc:creator>cabinet</dc:creator>` +
			`<cp:lastModifiedBy>cabinet</cp:lastModifiedBy><cp:revision>209</cp:revision>` +
			`<cp:lastPrinted>2100-01-01T00:00:00Z</cp:lastPrinted>` +
			`<dcterms:created xsi:type="dcterms:W3CDTF">2100-01-01T00:00:00Z</dcterms:created>` +
			`<dcterms:modified xsi:type="dcterms:W3CDTF">2100-01-01T00:00:00Z</dcterms:modified>` +
			`</cp:coreProperties>`,
		"docProps/app.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">` +
			`<TotalTime>392</TotalTime><Pages>6</Pages><Words>942</Words><Characters>5184</Characters>` +
			`<Lines>43</Lines><Paragraphs>12</Paragraphs><CharactersWithSpaces>6114</CharactersWithSpaces>` +
			`</Properties>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
			`<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/old-screenshot.png"/>` +
			`</Relationships>`,
		"word/numbering.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>` +
			`<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num></w:numbering>`,
		"word/footer2.xml": templateFooterXML,
		"word/settings.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:compat><w:compatSetting w:name="compatibilityMode"/></w:compat></w:settings>`,
		"word/media/old-screenshot.png": string(pngBytes(t, 4, 4)),
	}

	for name, data := range overrides {
		parts[name] = data
	}

	path := filepath.Join(t.TempDir(), "Template.docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// pngBytes builds a solid PNG of the requested size.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x60, B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writePNG drops an image of the requested size next to a Markdown file.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.WriteFile(path, pngBytes(t, w, h), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFile drops a Markdown file into a directory and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// repoFile points at a file kept at the repository root: Template.docx and the
// example/ directory. Go runs a test with its package directory as the working
// directory, so the root is two levels up.
func repoFile(name string) string { return filepath.Join("..", "..", name) }
