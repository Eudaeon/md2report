package docx

import (
	"path/filepath"
	"strings"
	"testing"
)

// everyBlock uses every style the contract names, so that filling it with the
// shipped template exercises the whole of what a template has to provide.
const everyBlock = "---\n" +
	"title: Incident report\ndate: 12 March 2026\ntype: Phishing\nreference: IR-COMPANY-002\n" +
	"---\n\n" +
	"# Part\n\n" +
	"Some text, **bold**, `code` and a [link](https://example.com/notice).\n\n" +
	"## Heading 2\n\n### Heading 3\n\n#### Heading 4\n\n##### Heading 5\n\n###### Heading 6\n\n" +
	"- a bullet\n\n1. a number\n\n> a quotation\n\n" +
	"```\na capture\n```\n\n" +
	"![A caption](capture.png)\n\n" +
	"| Time | Event |\n| --- | --- |\n| 14:37 | Email |\n"

// The shipped template has to hold up its end of the contract: every marker
// md2report fills, every style a report can ask for. Word rewrites content
// controls when a template is opened and saved again, which is exactly what
// happens the day somebody swaps the logo, and a template that quietly lost a
// marker fails at the worst possible moment, the one a report is due.
func TestShippedTemplateHoldsUpItsEndOfTheContract(t *testing.T) {
	b, err := openTemplate(repoFile("Template.docx"))
	if err != nil {
		t.Fatal(err)
	}
	lay, err := mapTemplate(b.get("word/document.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for tag, r := range markerRoles {
		if !lay.marks[r] {
			t.Errorf("Template.docx declares no %s marker", tag)
		}
	}

	// Every role resolves, and the shipped template declares no style property:
	// its own seven are named after the roles, and Word's eight are found under
	// the English name it writes beside its French ids, Titre2 for "heading 2"
	// and Citation for "Quote".
	st := resolveStyles(b.get("word/styles.xml"), b.get("docProps/custom.xml"))
	for role := range roles {
		if st.get(role) == "" {
			t.Errorf("Template.docx defines no style for the %s role", role)
		}
	}

	dir := t.TempDir()
	md := writeFile(t, dir, "report.md", everyBlock)
	writePNG(t, filepath.Join(dir, "capture.png"), 8, 8)

	out, warnings, err := Generate(md, repoFile("Template.docx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		t.Errorf("a report off the shipped template should warn about nothing, got: %s", w)
	}

	// The markers stay behind in the template they belong to.
	filled, err := openTemplate(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(filled.get("word/document.xml")); strings.Contains(got, "md2report:") {
		t.Error("the report carries a marker: they say what a template offers, and a report is not one")
	}

	// The page footer follows the front matter. It is the template's own
	// placeholder that shows up on every page of every report when it does not,
	// and the day somebody re-saves the template in Word is the day its footer
	// comes back spelled another way.
	footer := string(filled.get("word/footer2.xml"))
	if strings.Contains(footer, "IR-COMPANY-001") || !strings.Contains(footer, "IR-COMPANY-002") {
		t.Errorf("the footer keeps the template's reference instead of the report's:\n%s", footer)
	}
}
