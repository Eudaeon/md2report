package docx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"md2report/internal/markdown"
)

// style builds one <w:style> the way a template declares it.
func style(typ, id, name string) string {
	return `<w:style w:type="` + typ + `" w:styleId="` + id + `"><w:name w:val="` + name + `"/></w:style>`
}

func stylesXML(defs ...string) []byte {
	return []byte(`<w:styles>` + strings.Join(defs, "") + `</w:styles>`)
}

// customXML declares style properties the way Word writes them.
func customXML(pairs ...[2]string) []byte {
	s := `<Properties xmlns:vt="x">`
	for i, p := range pairs {
		s += `<property fmtid="{D5CDD505}" pid="` + string(rune('2'+i)) + `" name="md2report:style:` + p[0] + `">` +
			`<vt:lpwstr>` + p[1] + `</vt:lpwstr></property>`
	}
	return []byte(s + `</Properties>`)
}

// Word localizes the styleId of its own styles and keeps the English name in
// <w:name>. A template written in a French Word therefore needs to declare
// nothing for them: heading 2 is found under Titre2, Quote under Citation.
func TestResolveFindsBuiltInStylesInAnyLanguage(t *testing.T) {
	st := resolveStyles(stylesXML(
		style("paragraph", "Titre2", "heading 2"),
		style("paragraph", "berschrift3", "heading 3"),
		style("paragraph", "Citation", "Quote"),
		style("character", "Lienhypertexte", "Hyperlink"),
		style("table", "TableauNormal", "Normal Table"),
	), nil)

	for _, c := range []struct{ role, want string }{
		{styHeading(2), "Titre2"},
		{styHeading(3), "berschrift3"},
		{styQuote, "Citation"},
		{styHyperlink, "Lienhypertexte"},
		{styTable, "TableauNormal"},
	} {
		if got := st.get(c.role); got != c.want {
			t.Errorf("%s resolved to %q, expected the template's %q", c.role, got, c.want)
		}
	}
}

// A style the template author invented carries no English name, so a template
// spelling one its own way says which style it means.
func TestResolveFollowsADeclaredStyle(t *testing.T) {
	styles := stylesXML(
		style("paragraph", "Contenu", "Contenu"),
		style("paragraph", "Partie", "Partie"),
	)
	st := resolveStyles(styles, customXML([2]string{styBody, "Contenu"}, [2]string{styPart, "Partie"}))

	if got := st.get(styBody); got != "Contenu" {
		t.Errorf("Body resolved to %q, expected the declared Contenu", got)
	}
	if got := st.get(styPart); got != "Partie" {
		t.Errorf("Part resolved to %q, expected the declared Partie", got)
	}

	// Declaring a style the sheet never defines resolves to nothing, rather
	// than to a name Word would silently render in Normal.
	st = resolveStyles(styles, customXML([2]string{styBody, "Absente"}))
	if got := st.get(styBody); got != "" {
		t.Errorf("Body resolved to %q, expected nothing: the declared style is not defined", got)
	}
}

// A role is a name and a kind. A character style called Body is not the
// paragraph style a report's paragraphs are written in.
func TestResolveChecksTheKindOfStyle(t *testing.T) {
	st := resolveStyles(stylesXML(
		style("character", styBody, "Body"),
		style("paragraph", styLink, "Link"),
	), nil)

	if got := st.get(styBody); got != "" {
		t.Errorf("Body resolved to %q, expected nothing: that Body is a character style", got)
	}
	if got := st.get(styLink); got != "" {
		t.Errorf("Link resolved to %q, expected nothing: that Link is a paragraph style", got)
	}
}

// A template defining no table style is nobody's business until a report has a
// table in it.
func TestUnresolvedNamesOnlyWhatTheReportUsed(t *testing.T) {
	st := resolveStyles(stylesXML(style("paragraph", styBody, "Body")), nil)

	st.get(styBody)
	if got := st.unresolved(); len(got) != 0 {
		t.Errorf("unresolved = %v, expected nothing: the only style used is defined", got)
	}

	st.get(styTable)
	st.get(styPart)
	got := st.unresolved()
	if len(got) != 2 || got[0] != styTable || got[1] != styPart {
		t.Errorf("unresolved = %v, expected [NormalTable Part] sorted, and Bullet left out of it", got)
	}
}

// Word renders a paragraph whose style it cannot find in Normal, without a word
// of complaint, so md2report refuses rather than hand back a report stripped of
// its headings. The message names the property that would fix it.
func TestGenerateRefusesATemplateMissingAStyle(t *testing.T) {
	withoutPart := strings.Replace(templateStylesXML,
		`<w:style w:type="paragraph" w:styleId="`+styPart+`"><w:name w:val="`+styPart+`"/></w:style>`, "", 1)
	tpl := minimalTemplateWith(t, map[string]string{"word/styles.xml": withoutPart})

	dir := t.TempDir()
	md := filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, []byte("---\nreference: IR-A-1\n---\n\n# Summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Generate(md, tpl)
	if err == nil {
		t.Fatal("a template defining no Part style should be refused")
	}
	for _, want := range []string{styPart, "md2report:style:" + styPart} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q, got: %v", want, err)
		}
	}
}

// The same template, with the style declared under the name it uses, goes
// through.
func TestGenerateAcceptsADeclaredStyle(t *testing.T) {
	renamed := strings.Replace(templateStylesXML,
		`w:styleId="`+styPart+`"><w:name w:val="`+styPart+`"`,
		`w:styleId="Partie"><w:name w:val="Partie"`, 1)
	tpl := minimalTemplateWith(t, map[string]string{
		"word/styles.xml":     renamed,
		"docProps/custom.xml": string(customXML([2]string{styPart, "Partie"})),
	})

	dir := t.TempDir()
	md := filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, []byte("---\nreference: IR-A-1\n---\n\n# Summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := Generate(md, tpl)
	if err != nil {
		t.Fatalf("a declared style should be accepted: %v", err)
	}
	if !strings.Contains(readDocx(t, out)["word/document.xml"], `<w:pStyle w:val="Partie"/>`) {
		t.Error("the heading should be written in the style the template declared")
	}
}

// The bullet style carries its own numbering. Copying a fixed id instead would
// give another template's bullets whatever list happened to hold that number.
func TestBulletNumberingComesFromTheStyle(t *testing.T) {
	const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles>` +
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Listenumerotee"><w:pPr><w:numPr><w:numId w:val="9"/></w:numPr></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Bullet"><w:pPr><w:numPr><w:numId w:val="7"/></w:numPr></w:pPr></w:style>` +
		`<w:style w:type="character" w:customStyle="1" w:styleId="BulletCar"><w:pPr><w:numPr><w:numId w:val="4"/></w:numPr></w:pPr></w:style>` +
		`</w:styles>`

	if got := resolveStyles([]byte(styles), nil).bulletNum; got != 7 {
		t.Errorf("bullet numbering = %d, expected 7, the one the style names", got)
	}

	// A style naming no numbering, and no style sheet at all, both mean "none",
	// and the bullets then take whatever the style itself carries.
	none := stylesXML(`<w:style w:type="paragraph" w:styleId="Bullet"><w:pPr/></w:style>`)
	if got := resolveStyles(none, nil).bulletNum; got != 0 {
		t.Errorf("bullet numbering = %d, expected 0 when the style names none", got)
	}
	if got := resolveStyles(nil, nil).bulletNum; got != 0 {
		t.Errorf("bullet numbering = %d, expected 0 without a style sheet", got)
	}
}

// The caption format is house style, so it comes from the template. A template
// that says nothing gets the English default rather than an empty prefix.
func TestCaptionFormatComesFromTheTemplate(t *testing.T) {
	const props = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties"` +
		` xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
		`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="Company"><vt:lpwstr>Cabinet</vt:lpwstr></property>` +
		`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="3" name="md2report:caption"><vt:lpwstr>%s</vt:lpwstr></property>` +
		`</Properties>`

	cases := []struct {
		name   string
		custom []byte
		want   string
	}{
		{"no custom properties at all", nil, captionDefault},
		{
			"custom properties naming something else",
			[]byte(strings.Replace(props, "md2report:caption", "Manager", 1)),
			captionDefault,
		},
		{
			"a format spaced another way",
			[]byte(fmt.Sprintf(props, "Figure {n} : ")),
			"Figure {n} : ",
		},
		{
			"a format Word escaped on its way out",
			[]byte(fmt.Sprintf(props, "Fig. {n} &amp; suite &#8211; ")),
			"Fig. {n} & suite – ",
		},
		{
			"a format with no number in it",
			[]byte(fmt.Sprintf(props, "Illustration: ")),
			"Illustration: ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveStyles(nil, c.custom).caption; got != c.want {
				t.Errorf("caption format = %q, expected %q", got, c.want)
			}
		})
	}
}

// The image paragraph is written in the template's own style like every other
// block. A hard-coded name would ignore a template that spells it its own way,
// and would leave the role unchecked when the template defines none.
func TestImageUsesTheTemplatesStyle(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "one.png"), 8, 8)

	st := resolveStyles(stylesXML(style("paragraph", "Illustration", "Illustration")),
		customXML([2]string{styImage, "Illustration"}))
	body, _, err := newRenderer(&fakeResources{}, st, dir, 9072, 14004).body(
		[]markdown.Block{markdown.Image{Path: "one.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<w:pStyle w:val="Illustration"/>`) {
		t.Errorf("image paragraph = %s, expected the style the template declared", body)
	}
}

// The table of contents is optional. A template declaring no md2report:toc
// marker is never asked for the styles one is written in, since the roles a
// report never uses are not checked.
func TestATemplateOfferingNoTableOfContentsNeedsNoContentsStyle(t *testing.T) {
	doc := strings.Replace(templateDocumentXML, marked("toc", styled(styContents, "Ancienne entrée")), "", 1)
	styles := templateStylesXML
	for _, s := range [][2]string{{"paragraph", styContents}, {"character", styHyperlink}} {
		styles = strings.Replace(styles,
			`<w:style w:type="`+s[0]+`" w:styleId="`+s[1]+`"><w:name w:val="`+s[1]+`"/></w:style>`, "", 1)
	}
	tpl := minimalTemplateWith(t, map[string]string{"word/document.xml": doc, "word/styles.xml": styles})

	dir := t.TempDir()
	md := writeFile(t, dir, "report.md", "---\nreference: IR-A-1\n---\n\n# Summary\n")
	if _, _, err := Generate(md, tpl); err != nil {
		t.Fatalf("a template offering no table of contents should still fill: %v", err)
	}
}
