package markdown

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseFrontMatter(t *testing.T) {
	const src = "---\ntitle: Report\ndate: 1 August 2025\ntype: Phishing\nauthor: A. MARTIN\nreference: IR-ACME-001\n---\n\n# Summary\n"
	meta, body, _ := parseFrontMatter(src)
	if meta.Title != "Report" || meta.Date != "1 August 2025" || meta.Type != "Phishing" ||
		meta.Author != "A. MARTIN" || meta.Reference != "IR-ACME-001" {
		t.Fatalf("front matter misread: %+v", meta)
	}
	if body != "\n# Summary\n" {
		t.Errorf("body = %q, the front matter should be detached from it", body)
	}
	if unknown := Parse(src).Unknown; len(unknown) != 0 {
		t.Errorf("unresolved variables = %v, none expected", unknown)
	}
}
func TestParseFrontMatterMissing(t *testing.T) {
	src := "# Summary\n\nNo front matter here.\n"
	meta, body, _ := parseFrontMatter(src)
	if body != src {
		t.Errorf("body = %q, expected the whole document", body)
	}
	unknown := Parse(src).Unknown
	if meta.Reference != "" || len(meta.Vars) != 0 || unknown != nil {
		t.Errorf("Meta should stay empty: %+v, %v", meta, unknown)
	}
}
func TestVariables(t *testing.T) {
	doc := Parse("---\nreference: IR-COMPANY-002\nclient: COMPANY\nvictim: A. MARTIN\n---\n\n" +
		"# Compromise of {{victim}}\n\nIncident {{reference}} at {{client}} ({{undefined}}).\n")

	if doc.Meta.Reference != "IR-COMPANY-002" {
		t.Errorf("reference = %q, a variable should have been expanded in it", doc.Meta.Reference)
	}
	if got := Plain(doc.Blocks[0].(Heading).Inlines); got != "Compromise of A. MARTIN" {
		t.Errorf("heading = %q", got)
	}
	if got := Plain(doc.Blocks[1].(Paragraph).Inlines); got != "Incident IR-COMPANY-002 at COMPANY ({{undefined}})." {
		t.Errorf("paragraph = %q; an unknown variable must stay visible", got)
	}
	if len(doc.Unknown) != 1 || doc.Unknown[0] != "undefined" {
		t.Errorf("unknown variables = %v, expected [undefined]", doc.Unknown)
	}
}
func TestChainedVariables(t *testing.T) {
	doc := Parse("---\nreference: \"IR-{{client}}-007\"\nclient: \"{{group}}-FR\"\ngroup: ACME\ncycle: \"{{cycle}}\"\n---\n\nSome text.\n")
	meta, unknown := doc.Meta, doc.Unknown
	if meta.Reference != "IR-ACME-FR-007" {
		t.Errorf("reference = %q: variables must expand whatever their order", meta.Reference)
	}
	if len(unknown) != 1 || unknown[0] != "cycle" {
		t.Errorf("unresolved variables = %v, expected [cycle]", unknown)
	}
}

// Three hundred links is far past anything a report will hold. The depth is the
// point: the README promises no limit, so the test picks one no bounded number
// of passes would reach. The cycle beside it must not drag the chain down.
func TestDeepChainAndIndirectCycle(t *testing.T) {
	var src strings.Builder
	src.WriteString("---\nreference: \"IR-{{v0}}-007\"\ntic: \"{{tac}}\"\ntac: \"{{tic}}\"\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&src, "v%d: \"{{v%d}}\"\n", i, i+1)
	}
	src.WriteString("v300: ACME\n---\n\nSome text.\n")

	doc := Parse(src.String())
	meta, unknown := doc.Meta, doc.Unknown
	if meta.Reference != "IR-ACME-007" {
		t.Errorf("reference = %q: a chain must expand however deep it runs", meta.Reference)
	}
	// Both members of the cycle are named, and neither is rewritten to point at
	// the other, so the warning and the document agree with the front matter.
	if len(unknown) != 2 || unknown[0] != "tac" || unknown[1] != "tic" {
		t.Errorf("unresolved variables = %v, expected both halves of the cycle", unknown)
	}
	if meta.Vars["tic"] != "{{tac}}" || meta.Vars["tac"] != "{{tic}}" {
		t.Errorf("cycle = tic:%q tac:%q, both must stay written as they are",
			meta.Vars["tic"], meta.Vars["tac"])
	}
}

// Only the reference key names the incident. Its value is declared outright,
// though it may be built from variables of its own like any other value.
func TestReferenceIsDeclared(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"declared", "---\nreference: IR-ACME-007\n---\n", "IR-ACME-007"},
		{"a scheme of its own", "---\nreference: INC-2026-014\n---\n", "INC-2026-014"},
		{"assembled from variables", "---\nreference: \"IR-{{client}}-007\"\nclient: ACME\n---\n", "IR-ACME-007"},
		{"an accented spelling is just a variable", "---\nréférence: IR-ACME-007\n---\n", ""},
		{"none", "---\ntitle: Report\n---\n", ""},
	}
	for _, c := range cases {
		meta, _, _ := parseFrontMatter(c.src)
		if meta.Reference != c.want {
			t.Errorf("%s: reference = %q, expected %q", c.name, meta.Reference, c.want)
		}
	}
}

// A variable may quote {{reference}}, which is itself assembled from another.
func TestReferenceUsableInOtherVariables(t *testing.T) {
	doc := Parse("---\nreference: \"IR-{{group}}-007\"\ngroup: ACME\n" +
		"title: \"Incident {{reference}}\"\n---\n\nSee {{reference}}.\n")
	if doc.Meta.Title != "Incident IR-ACME-007" {
		t.Errorf("title = %q, a variable should be able to quote {{reference}}", doc.Meta.Title)
	}
	if got := Plain(doc.Blocks[0].(Paragraph).Inlines); got != "See IR-ACME-007." {
		t.Errorf("paragraph = %q, {{reference}} should expand in the body too", got)
	}
}

// A report is not written in English, so a key may carry an accent.
func TestVariableNamesMayHoldAnyLetters(t *testing.T) {
	doc := Parse("---\nreference: IR-A-1\nprénom: Lucie\nsociété: \"{{prénom}} SA\"\n---\n\nBonjour {{prénom}} de {{société}}, {{oublié}}.\n")
	if got := Plain(doc.Blocks[0].(Paragraph).Inlines); got != "Bonjour Lucie de Lucie SA, {{oublié}}." {
		t.Errorf("paragraph = %q: accented variables must expand like any other", got)
	}
	if len(doc.Unknown) != 1 || doc.Unknown[0] != "oublié" {
		t.Errorf("unknown = %v, expected [oublié]: an accented name must warn too", doc.Unknown)
	}
}
