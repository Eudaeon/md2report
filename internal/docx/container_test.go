package docx

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A template may already carry the setting, and carry it turned off. Leaving it
// alone would leave the table of contents showing the template's page numbers.
func TestUpdateFieldsIsTurnedOnWhateverTheTemplateSays(t *testing.T) {
	const head = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`
	cases := []struct {
		name     string
		settings string
	}{
		{"absent", head + `<w:compat/></w:settings>`},
		{"turned off", head + `<w:updateFields w:val="false"/><w:compat/></w:settings>`},
		{"turned off, in a pair of tags", head + `<w:updateFields w:val="false"></w:updateFields><w:compat/></w:settings>`},
		{"already on", head + `<w:updateFields w:val="true"/><w:compat/></w:settings>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &builder{parts: []part{{name: "word/settings.xml", data: []byte(c.settings)}}}
			b.applyUpdateFields()

			got := string(b.get("word/settings.xml"))
			if n := strings.Count(got, "<w:updateFields"); n != 1 {
				t.Fatalf("the setting appears %d times, expected once: %s", n, got)
			}
			if !strings.Contains(got, `<w:updateFields w:val="true"/>`) {
				t.Errorf("the setting should be on: %s", got)
			}
			if strings.Contains(got, "</w:updateFields>") {
				t.Errorf("a closing tag was left with nothing to close: %s", got)
			}
		})
	}
}

// A template carrying no settings part gets one, since nothing else would ask
// Word to refresh the table of contents.
func TestSettingsPartIsCreatedWhenTheTemplateHasNone(t *testing.T) {
	dir := t.TempDir()
	md := writeFile(t, dir, "r.md", "---\nreference: IR-A-3\n---\n\n# Heading\n\nSome text.\n")

	stripped := filepath.Join(t.TempDir(), "sans-settings.docx")
	dropPart(t, minimalTemplate(t), "word/settings.xml", stripped)

	out, _, err := Generate(md, stripped)
	if err != nil {
		t.Fatalf("filling a template without settings: %v", err)
	}
	parts := readDocx(t, out)

	if !strings.Contains(parts["word/settings.xml"], `<w:updateFields w:val="true"/>`) {
		t.Errorf("settings part = %q, expected the setting turned on", parts["word/settings.xml"])
	}
	if !strings.Contains(parts["[Content_Types].xml"], `PartName="/word/settings.xml"`) {
		t.Error("the new part's content type was not declared")
	}
	if !strings.Contains(parts["word/_rels/document.xml.rels"], "settings.xml") {
		t.Error("nothing relates the document to its new settings part")
	}
}

// A template that never held a numbered list carries no numbering part. Filling
// it used to succeed and produce a document whose list numbers pointed at
// nothing, so the lists came out unnumbered with no warning.
func TestTemplateWithoutNumberingIsRefused(t *testing.T) {
	dir := t.TempDir()
	md := writeFile(t, dir, "r.md", "---\nreference: IR-A-1\n---\n\n# Titre\n\n1. un\n2. deux\n")

	tpl := minimalTemplate(t)
	stripped := filepath.Join(t.TempDir(), "sans-numerotation.docx")
	dropPart(t, tpl, "word/numbering.xml", stripped)

	_, _, err := Generate(md, stripped)
	if err == nil || !strings.Contains(err.Error(), "numbering.xml") {
		t.Fatalf("error = %v, expected one naming the missing numbering part", err)
	}

	// A report with no numbered list has no need of the part, and still fills.
	plain := writeFile(t, dir, "plain.md", "---\nreference: IR-A-2\n---\n\n# Heading\n\nSome text.\n")
	if _, _, err := Generate(plain, stripped); err != nil {
		t.Errorf("a report without numbered lists should not need the part: %v", err)
	}
}

// dropPart copies a .docx to dst, leaving one part out.
func dropPart(t *testing.T, src, name, dst string) {
	t.Helper()
	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, p := range zr.File {
		if p.Name == name {
			continue
		}
		w, err := zw.Create(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := p.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// The parser indents a nested list up to level 4, so the numbering has to define
// levels 0 to 4. A paragraph pointing at a level nothing defines takes its
// indent and its number format from nowhere.
func TestNumberingDefinesEveryLevelANestedListCanReach(t *testing.T) {
	dir := t.TempDir()
	md := writeFile(t, dir, "r.md", "---\nreference: IR-A-1\n---\n\n# Titre\n\n"+
		"1. un\n  1. deux\n    1. trois\n      1. quatre\n        1. cinq\n")

	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	parts := readDocx(t, out)

	// Only the levels of the lists we number ourselves; the bullets take theirs
	// from the template's own numbering.
	ours := map[string]bool{}
	for _, m := range regexp.MustCompile(`<w:ilvl w:val="(\d+)"/><w:numId w:val="(\d+)"/>`).
		FindAllStringSubmatch(parts["word/document.xml"], -1) {
		if strings.Contains(parts["word/numbering.xml"], `<w:num w:numId="`+m[2]+`">`) {
			ours[m[1]] = true
		}
	}
	if len(ours) != 5 {
		t.Fatalf("the document uses %d nesting levels, expected 5: %v", len(ours), ours)
	}
	for lvl := range ours {
		if !strings.Contains(parts["word/numbering.xml"], `<w:lvl w:ilvl="`+lvl+`">`) {
			t.Errorf("the document numbers a list at level %s, which numbering.xml never defines", lvl)
		}
	}
}

// Screenshots are the bulk of what a report weighs, and a diagram shown in every
// section used to be carried once per mention.
func TestAddImageStoresOneCopyPerFile(t *testing.T) {
	b := &builder{nextRel: 1000, links: map[string]string{}, media: map[string]string{},
		parts: []part{{name: "[Content_Types].xml", data: []byte(`<Types><Default Extension="xml"/></Types>`)}}}

	logo := []byte("\x89PNG-pretend-this-is-a-logo")
	first := b.addImage(logo, "png", "image/png")
	again := b.addImage(logo, "png", "image/png")
	other := b.addImage([]byte("\x89PNG-a-different-picture"), "png", "image/png")

	if first != again {
		t.Errorf("the same image gave %s then %s, expected one relationship", first, again)
	}
	if other == first {
		t.Errorf("a different image gave %s, the same as the first", other)
	}
	media := 0
	for _, p := range b.parts {
		if strings.HasPrefix(p.name, "word/media/") {
			media++
		}
	}
	if media != 2 {
		t.Errorf("%d media parts, expected 2", media)
	}
}

// A report is confidential until its author says otherwise, including when an
// earlier run left a wider-open file in place.
func TestWriteKeepsTheDocumentPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.docx")
	if err := os.WriteFile(path, []byte("an earlier run"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &builder{parts: []part{{name: "word/document.xml", data: []byte("<w:document/>")}}}
	if err := b.write(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, expected 600", got)
	}
}
