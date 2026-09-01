package docx

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestOutputFileName(t *testing.T) {
	dir := t.TempDir()
	md := writeFile(t, dir, "note.md", "---\nreference: IR-COMPANY/A-003\n---\n\n# Summary\n\nSome text.\n")

	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "IR-COMPANY-A-003.docx"); out != want {
		t.Errorf("output = %q, expected %q (reference sanitised)", out, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}

	// With no reference the document cannot be named.
	noRef := writeFile(t, t.TempDir(), "no-reference.md", "Some text.\n")
	if _, _, err := Generate(noRef, minimalTemplate(t)); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Errorf("an error was expected when the reference is missing, got: %v", err)
	}
}

// TestGeneratedDocument fills the minimal template and checks the internal
// consistency of the result: relationships, numberings, bookmarks, fields.
func TestGeneratedDocument(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "connections.png"), 400, 200)
	md := writeFile(t, dir, "report.md", `---
title: Incident report
date: Thursday 25 July 2025
type: Phishing
reference: IR-COMPANY-002
---

# Summary

An email sent from the victim's account ([contact](mailto:c@example.com)).

## Detail

- a bullet
- another

1. first step
2. second step

| Date | Event |
| --- | --- |
| 25/07 | Sign-in |

![Sign-ins](connections.png){width=70%}

# Recommendations

> Reset the credentials.
`)

	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "IR-COMPANY-002.docx" {
		t.Errorf("the document should be named after the reference, got %q", filepath.Base(out))
	}

	parts := readDocx(t, out)
	doc := parts["word/document.xml"]

	for _, want := range []string{
		`<w:pStyle w:val="` + styPart + `"/>`,
		`<w:pStyle w:val="` + styHeading(2) + `"/>`,
		`<w:pStyle w:val="` + styBody + `"/>`,
		`<w:pStyle w:val="` + styBullet + `"/>`,
		`<w:pStyle w:val="` + styQuote + `"/>`,
		`<w:pStyle w:val="` + styFigureCaption + `"/>`,
		`<w:pStyle w:val="` + styContents + `"/>`,
		`IR-COMPANY-002`,
		`Thursday 25 July 2025`,
		`<w:tbl>`,
		`<w:drawing>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the document does not contain %s", want)
		}
	}
	// The template's cover page really was rewritten.
	for _, gone := range []string{"MODELE-000", "Vendredi 1 janvier 2100", "Ancienne entrée"} {
		if strings.Contains(doc, gone) {
			t.Errorf("%q: the template's value should have been replaced", gone)
		}
	}

	// The image asks for 70% of the usable width (11906 - 2×1417 twips).
	extents := regexp.MustCompile(`<wp:extent cx="(\d+)"`).FindAllStringSubmatch(doc, -1)
	if len(extents) != 1 {
		t.Fatalf("%d image(s) in the document, 1 expected", len(extents))
	}
	if want := strconv.Itoa(9072 * emuPerTwip * 70 / 100); extents[0][1] != want {
		t.Errorf("image width = %s, expected %s", extents[0][1], want)
	}

	checkConsistency(t, parts)

	// The template's media, now unreferenced, is gone; ours is there.
	if _, ok := parts["word/media/old-screenshot.png"]; ok {
		t.Error("the template's image is no longer referenced and should have been dropped")
	}
	if _, ok := parts["word/media/md-image-1.png"]; !ok {
		t.Error("the Markdown's image is missing from the document")
	}
	if !strings.Contains(parts["[Content_Types].xml"], `Extension="png"`) {
		t.Error("the PNG MIME type was not declared")
	}
	if !strings.Contains(parts["word/settings.xml"], "w:updateFields") {
		t.Error("Word should be asked to recompute the fields on opening")
	}
}

// Front matter holding nothing but the reference leaves the other cover fields
// untouched.
func TestCoverFieldsKeptFromTemplate(t *testing.T) {
	md := writeFile(t, t.TempDir(), "note.md", "---\nreference: IR-ACME-002\n---\n\nJuste un paragraphe, sans titre ni section.\n")
	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	doc := readDocx(t, out)["word/document.xml"]

	if !strings.Contains(doc, "Juste un paragraphe") {
		t.Error("paragraph missing from the document")
	}
	if !strings.Contains(doc, "IR-ACME-002") {
		t.Error("the front matter's reference should appear on the cover page")
	}
	for _, fromTemplate := range []string{"Titre du modèle", "Vendredi 1 janvier 2100", "Hameçonnage"} {
		if !strings.Contains(doc, fromTemplate) {
			t.Errorf("%q: cover fields absent from the front matter must keep the template's value", fromTemplate)
		}
	}
	// With no level-1 heading the table of contents is empty but well formed.
	if strings.Count(doc, `fldCharType="begin"`) != strings.Count(doc, `fldCharType="end"`) {
		t.Error("unbalanced TOC field")
	}
}

func TestMissingImage(t *testing.T) {
	md := writeFile(t, t.TempDir(), "note.md", "---\nreference: IR-ACME-003\n---\n\n![Caption](missing.png)\n")
	if _, _, err := Generate(md, minimalTemplate(t)); err == nil || !strings.Contains(err.Error(), "missing.png") {
		t.Fatalf("an error was expected for a missing image, got: %v", err)
	}
}

func TestInvalidTemplate(t *testing.T) {
	notADocx := writeFile(t, t.TempDir(), "template.docx", "this is not a ZIP")
	md := writeFile(t, t.TempDir(), "note.md", "---\nreference: IR-ACME-004\n---\n\nSome text.\n")
	if _, _, err := Generate(md, notADocx); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("an error was expected for an unreadable template, got: %v", err)
	}
}

// TestExampleWithRealTemplate puts the repository's example through the real
// template: the only check that depends on files kept at the repository root.
// Either one may be absent from a checkout, so both are looked for.
func TestExampleWithRealTemplate(t *testing.T) {
	for _, name := range []string{"Template.docx", "example/report.md"} {
		if _, err := os.Stat(repoFile(name)); err != nil {
			t.Skip(name + " absent")
		}
	}
	out, _, err := Generate(copyExample(t), repoFile("Template.docx"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "IR-COMPANY-002.docx" {
		t.Errorf("the document should be named after the reference, got %q", filepath.Base(out))
	}

	parts := readDocx(t, out)
	doc := parts["word/document.xml"]
	if strings.Contains(doc, "hearingplus.com.au/edocument") && !strings.Contains(doc, "The link in the email") {
		t.Error("the template's original content was not replaced")
	}
	// The example's second image asks for 70% of the usable width.
	extents := regexp.MustCompile(`<wp:inline[^>]*><wp:extent cx="(\d+)"`).FindAllStringSubmatch(doc, -1)
	if len(extents) != 2 {
		t.Fatalf("%d image(s) in the document, 2 expected", len(extents))
	}
	if want := strconv.Itoa(8064 * emuPerTwip * 70 / 100); extents[1][1] != want {
		t.Errorf("second image width = %s, expected %s", extents[1][1], want)
	}
	checkConsistency(t, parts)
}

// checkConsistency verifies that a produced document refers to nothing that does
// not exist: relationships, numberings, table-of-contents bookmarks, Word
// fields.
func checkConsistency(t *testing.T, parts map[string]string) {
	t.Helper()
	doc := parts["word/document.xml"]

	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`Id="([^"]+)"`).FindAllStringSubmatch(parts["word/_rels/document.xml.rels"], -1) {
		ids[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`r:(?:id|embed)="([^"]+)"`).FindAllStringSubmatch(doc, -1) {
		if !ids[m[1]] {
			t.Errorf("relationship %s referenced but absent", m[1])
		}
	}

	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`<w:num w:numId="(\d+)"`).FindAllStringSubmatch(parts["word/numbering.xml"], -1) {
		defined[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`<w:numId w:val="(\d+)"/>`).FindAllStringSubmatch(doc, -1) {
		if !defined[m[1]] {
			t.Errorf("numbering %s referenced but absent", m[1])
		}
	}

	marks := map[string]bool{}
	for _, m := range regexp.MustCompile(`<w:bookmarkStart w:id="\d+" w:name="([^"]+)"`).FindAllStringSubmatch(doc, -1) {
		marks[m[1]] = true
	}
	anchors := regexp.MustCompile(`w:anchor="([^"]+)"`).FindAllStringSubmatch(doc, -1)
	if len(anchors) == 0 {
		t.Error("empty table of contents")
	}
	for _, m := range anchors {
		if !marks[m[1]] {
			t.Errorf("entry %s points at no bookmark", m[1])
		}
	}

	if a, b := strings.Count(doc, `fldCharType="begin"`), strings.Count(doc, `fldCharType="end"`); a != b {
		t.Errorf("unbalanced fields: %d start(s) for %d end(s)", a, b)
	}
}

// copyExample copies the example out of the repository: the produced document is
// named after the reference and must not land in the sources.
func copyExample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"report.md", "images/connections.png", "images/mfa.png"} {
		data, err := os.ReadFile(filepath.Join(repoFile("example"), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "report.md")
}

func readDocx(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	parts := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(data)
	}
	return parts
}

// The reference is repeated in the page footer, in a text box Word stores twice.
// Both copies follow the front matter; a sentence that merely cites an older
// incident does not.
func TestFooterReference(t *testing.T) {
	md := writeFile(t, t.TempDir(), "note.md",
		"---\nreference: IR-COMPANY-042\n---\n\n# Summary\n\nSome text.\n")
	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	footer := readDocx(t, out)["word/footer2.xml"]

	if n := strings.Count(footer, "<w:t>IR-COMPANY-042</w:t>"); n != 2 {
		t.Errorf("the reference was rewritten in %d text box(es), 2 expected:\n%s", n, footer)
	}
	if !strings.Contains(footer, "Fait suite à l'incident MODELE-000, classé sans suite.") {
		t.Errorf("a sentence citing another incident must be left alone:\n%s", footer)
	}
}

// Until the tool rewrites them, the properties Word lists under File > Properties
// are the template's: its revision, its authoring time, the day it was last
// printed, the length of a body the report has replaced. The author is the one
// the report keeps, as long as the front matter names none of its own.
func TestDocumentPropertiesDescribeTheReport(t *testing.T) {
	md := writeFile(t, t.TempDir(), "note.md",
		"---\ntitle: Incident report\nreference: IR-COMPANY-002\n---\n\n# Summary\n\nSome text.\n")
	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	parts := readDocx(t, out)

	core := parts["docProps/core.xml"]
	for _, want := range []string{
		`<dc:title>Incident report</dc:title>`,
		`<dc:subject>IR-COMPANY-002</dc:subject>`,
		`<cp:revision>1</cp:revision>`,
		`<dc:creator>cabinet</dc:creator>`,
		`<cp:lastModifiedBy>cabinet</cp:lastModifiedBy>`,
	} {
		if !strings.Contains(core, want) {
			t.Errorf("core.xml does not contain %s:\n%s", want, core)
		}
	}
	if strings.Contains(core, "lastPrinted") {
		t.Errorf("the day the template was printed should be gone:\n%s", core)
	}
	if strings.Contains(core, "2100-01-01") {
		t.Errorf("the template's timestamps should have been replaced:\n%s", core)
	}

	app := parts["docProps/app.xml"]
	for _, gone := range []string{"<TotalTime>392", "<Pages>6", "<Words>942", "<CharactersWithSpaces>6114"} {
		if strings.Contains(app, gone) {
			t.Errorf("app.xml still counts the template's body, %s:\n%s", gone, app)
		}
	}
}

// A report is filed under the analyst who wrote it, when the front matter says
// so. Both properties move together: Word shows one as the author and the other
// as whoever saved the file last, and a report the template still claims to have
// touched last would contradict its own author.
func TestFrontMatterNamesTheAuthor(t *testing.T) {
	md := writeFile(t, t.TempDir(), "note.md",
		"---\ntitle: Incident report\nauthor: A. MARTIN\nreference: IR-COMPANY-002\n---\n\n# Summary\n\nWritten by {{author}}.\n")
	out, _, err := Generate(md, minimalTemplate(t))
	if err != nil {
		t.Fatal(err)
	}
	parts := readDocx(t, out)

	core := parts["docProps/core.xml"]
	for _, want := range []string{`<dc:creator>A. MARTIN</dc:creator>`, `<cp:lastModifiedBy>A. MARTIN</cp:lastModifiedBy>`} {
		if !strings.Contains(core, want) {
			t.Errorf("core.xml does not contain %s:\n%s", want, core)
		}
	}
	if strings.Contains(core, "cabinet") {
		t.Errorf("the template's author should have been replaced:\n%s", core)
	}
	// Nothing sets the key apart from the others: it is a variable too.
	if !strings.Contains(parts["word/document.xml"], "Written by A. MARTIN.") {
		t.Error("{{author}} should expand in the body like any other front-matter key")
	}
}
