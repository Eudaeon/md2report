package docx

import (
	"md2report/internal/markdown"
	"strings"
	"testing"
)

func TestMapTemplate(t *testing.T) {
	lay, err := mapTemplate([]byte(templateDocumentXML))
	if err != nil {
		t.Fatal(err)
	}

	// 11906 - 1417 - 1417 wide, 16838 - 1417 - 1417 high.
	if w, h := lay.width, lay.height; w != 9072 || h != 14004 {
		t.Errorf("usable area = %d×%d twips, expected 9072×14004", w, h)
	}

	meta := markdown.Meta{Title: "Incident report", Date: "Thursday 25 July 2025", Type: "Phishing", Reference: "IR-TEST-001"}
	out := string(lay.splice(meta, "<w:p>TOC</w:p>", "<w:p>BODY</w:p>", styContents))

	for _, want := range []string{"Incident report", "Thursday 25 July 2025", "Phishing", "IR-TEST-001"} {
		if !strings.Contains(out, want) {
			t.Errorf("cover field %q missing from the filled document", want)
		}
	}
	for _, gone := range []string{"Titre du modèle", "Vendredi 1 janvier 2100", "Hameçonnage", "MODELE-000", "Ancienne entrée"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q: the template's value should have been replaced", gone)
		}
	}
	// The labels themselves stay.
	for _, want := range []string{"Date de parution", "Type d'incident", "Référence"} {
		if !strings.Contains(out, want) {
			t.Errorf("label %q lost", want)
		}
	}

	// The template's own body goes; what it puts after the body stays.
	if strings.Contains(out, "Corps du modèle") {
		t.Error("the template's placeholder body should have been replaced")
	}
	toc, body := strings.Index(out, "<w:p>TOC</w:p>"), strings.Index(out, "<w:p>BODY</w:p>")
	tail, sect := strings.Index(out, "Mentions légales"), strings.Index(out, "<w:sectPr>")
	if toc < 0 || body < 0 || tail < 0 || sect < 0 {
		t.Fatalf("table of contents, body, trailing content or sectPr missing from the filled document")
	}
	if !(toc < body && body < tail && tail < sect) {
		t.Errorf("expected order: contents, body, trailing content, sectPr (got %d, %d, %d, %d)", toc, body, tail, sect)
	}

	// A marker says what a template offers. A report is not a template.
	if strings.Contains(out, "md2report:") {
		t.Error("the filled document should carry none of the template's markers")
	}
	if !strings.HasSuffix(out, "</w:body></w:document>") {
		t.Errorf("the tail of the document was not copied through: %q", out[max(0, len(out)-40):])
	}
}

// A field the front matter does not declare keeps the template's value.
func TestSpliceKeepsUnsetCoverFields(t *testing.T) {
	lay, err := mapTemplate([]byte(templateDocumentXML))
	if err != nil {
		t.Fatal(err)
	}
	out := string(lay.splice(markdown.Meta{Reference: "IR-TEST-002"}, "", "", styContents))

	if !strings.Contains(out, "IR-TEST-002") {
		t.Error("a declared reference should replace the template's own")
	}
	for _, want := range []string{"Titre du modèle", "Vendredi 1 janvier 2100", "Hameçonnage"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q: a field absent from the front matter must keep the template's value", want)
		}
	}
}

// An unmarked .docx is not a template. Refusing it is the whole point: filling
// one would produce a document that looks like a report and says the wrong
// things, which is worse than producing nothing.
func TestMapTemplateNeedsItsMarkers(t *testing.T) {
	const bare = `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>Nothing recognisable</w:t></w:r></w:p></w:body></w:document>`
	if _, err := mapTemplate([]byte(bare)); err == nil || !strings.Contains(err.Error(), "md2report:reference") {
		t.Fatalf("an unmarked document should be refused by name, got: %v", err)
	}

	// A cover it can fill, but nowhere to put the report.
	noBody := strings.Replace(templateDocumentXML, `<w:tag w:val="md2report:body"/>`, "", 1)
	if _, err := mapTemplate([]byte(noBody)); err == nil || !strings.Contains(err.Error(), "md2report:body") {
		t.Fatalf("a template with no body marker should be refused by name, got: %v", err)
	}

	if _, err := mapTemplate([]byte(`<w:document xmlns:w="w"/>`)); err == nil {
		t.Fatal("an error was expected when <w:body> is absent")
	}
}

// A cover field a template does not offer is a fair design. The same field with
// a value waiting for it is a template that would swallow the value in silence.
func TestCheckCoverRefusesAValueItCannotPlace(t *testing.T) {
	noDate := strings.Replace(templateDocumentXML, `<w:tag w:val="md2report:date"/>`, "", 1)
	lay, err := mapTemplate([]byte(noDate))
	if err != nil {
		t.Fatal(err)
	}
	if err := lay.checkCover(markdown.Meta{Reference: "IR-TEST-003"}); err != nil {
		t.Errorf("a field the front matter leaves out needs no marker: %v", err)
	}
	err = lay.checkCover(markdown.Meta{Reference: "IR-TEST-003", Date: "12 mars 2026"})
	if err == nil || !strings.Contains(err.Error(), "md2report:date") {
		t.Fatalf("a date with no marker to hold it should be refused by name, got: %v", err)
	}
}

// Without <w:sectPr> the usable area falls back to the defaults.
func TestMapTemplateWithoutSectPr(t *testing.T) {
	without := strings.Replace(templateDocumentXML,
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417"/></w:sectPr>`, "", 1)
	lay, err := mapTemplate([]byte(without))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := lay.width, lay.height; w != defaultContentW || h != defaultContentH {
		t.Errorf("usable area = %d×%d, expected the defaults %d×%d", w, h, defaultContentW, defaultContentH)
	}
}
