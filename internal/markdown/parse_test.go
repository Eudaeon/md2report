package markdown

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseBlocks(t *testing.T) {
	src := `# Heading

A paragraph
over two lines.

- item one
  continued item
- item two

1. first
2. second

![Caption](img.png)

| A | B |
| --- | --- |
| 1 | 2 |

> a quotation

` + "```\nsome code\n```\n"

	blocks := parseBlocks(src)
	got := make([]string, len(blocks))
	for i, b := range blocks {
		got[i] = fmt.Sprintf("%T", b)
	}
	want := []string{
		"markdown.Heading", "markdown.Paragraph", "markdown.Bullet", "markdown.Bullet",
		"markdown.Ordered", "markdown.Ordered", "markdown.Image", "markdown.Table",
		"markdown.Quote", "markdown.Code",
	}
	if len(got) != len(want) {
		t.Fatalf("blocks = %v, expected %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block %d = %s, expected %s", i, got[i], want[i])
		}
	}

	if txt := Plain(blocks[1].(Paragraph).Inlines); txt != "A paragraph over two lines." {
		t.Errorf("joined paragraph = %q", txt)
	}
	if txt := Plain(blocks[2].(Bullet).Inlines); txt != "item one continued item" {
		t.Errorf("list continuation = %q", txt)
	}
	if blocks[4].(Ordered).ListID != blocks[5].(Ordered).ListID {
		t.Errorf("the two numbered items should share a list")
	}
	img := blocks[6].(Image)
	if img.Path != "img.png" || Plain(img.Caption) != "Caption" {
		t.Errorf("image = %+v", img)
	}
	if rows := blocks[7].(Table).Rows; len(rows) != 2 || len(rows[0]) != 2 {
		t.Errorf("table = %+v", rows)
	}
	if code := blocks[9].(Code).Text; code != "some code" {
		t.Errorf("code block = %q", code)
	}
}

// Ordered lists separated by another block start afresh.
func TestConsecutiveOrderedLists(t *testing.T) {
	blocks := parseBlocks("1. one\n2. two\n\nSome text.\n\n1. three\n")
	a := blocks[0].(Ordered).ListID
	b := blocks[len(blocks)-1].(Ordered).ListID
	if a == b {
		t.Errorf("two distinct lists share ListID %d", a)
	}
}

// A blank line between items spaces a list out; it does not start a new one,
// and a list that started afresh would be numbered from 1 again.
func TestALooseOrderedListIsStillOneList(t *testing.T) {
	blocks := parseBlocks("1. one\n\n2. two\n\n3. three\n")
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(blocks))
	}
	first := blocks[0].(Ordered).ListID
	for i, b := range blocks {
		if id := b.(Ordered).ListID; id != first {
			t.Errorf("item %d has ListID %d, want %d: the list restarts at 1 there", i, id, first)
		}
	}
}

func TestParseImageWidth(t *testing.T) {
	cases := []struct {
		src  string
		want ImageWidth
	}{
		{"![L](a.png)", ImageWidth{}},
		{"![L](a.png){width=60%}", ImageWidth{Value: 60, Unit: "%"}},
		{"![L](a.png){width=480px}", ImageWidth{Value: 480, Unit: "px"}},
		{"![L](a.png){ width=480 }", ImageWidth{Value: 480, Unit: "px"}},
		{"![L](a.png){width=8cm}", ImageWidth{Value: 8, Unit: "cm"}},
		{"![L](a.png){width=2in}", ImageWidth{Value: 2, Unit: "in"}},
		{"![L](a.png){width=36pt}", ImageWidth{Value: 36, Unit: "pt"}},
		{`![L](a.png "titre"){width="50%"}`, ImageWidth{Value: 50, Unit: "%"}},
	}
	for _, c := range cases {
		path, caption, got, ok := parseImageLine(c.src)
		if !ok || path != "a.png" || caption != "L" {
			t.Errorf("%s: parsing failed (path=%q caption=%q ok=%v)", c.src, path, caption, ok)
			continue
		}
		if got != c.want {
			t.Errorf("%s: width = %+v, expected %+v", c.src, got, c.want)
		}
	}
	if _, _, _, ok := parseImageLine("![L](a.png) followed by text"); ok {
		t.Error("an image followed by text must not be read as an image block")
	}
}

func TestImagePathWithParentheses(t *testing.T) {
	path, _, w, ok := parseImageLine("![L](images/a(b).png){width=60%}")
	if !ok || path != "images/a(b).png" {
		t.Errorf("path = %q (ok=%v), expected images/a(b).png", path, ok)
	}
	if w.Unit != "%" || w.Value != 60 {
		t.Errorf("width = %+v: the attributes still follow the path", w)
	}
}

// A table needs no blank line above it: the paragraph must end where the table
// begins, or the whole table is swallowed into it as run-on text.
func TestTableEndsTheParagraphAboveIt(t *testing.T) {
	doc := Parse("Affected accounts:\n| account | date |\n|---|---|\n| a | 1 |\n")
	if len(doc.Blocks) != 2 {
		t.Fatalf("blocks = %d, expected a paragraph then a table: %+v", len(doc.Blocks), doc.Blocks)
	}
	if got := Plain(doc.Blocks[0].(Paragraph).Inlines); got != "Affected accounts:" {
		t.Errorf("paragraph = %q, it should stop at the table", got)
	}
	tbl, ok := doc.Blocks[1].(Table)
	if !ok {
		t.Fatalf("second block = %T, expected a Table", doc.Blocks[1])
	}
	if len(tbl.Rows) != 2 {
		t.Errorf("rows = %d, expected the header and one row", len(tbl.Rows))
	}
}

// A pipe inside ordinary prose is not a table, and must not cut the paragraph.
func TestAPipeAloneIsNotATable(t *testing.T) {
	doc := Parse("The command a | b failed\nand so did the next one.\n")
	if len(doc.Blocks) != 1 {
		t.Fatalf("blocks = %d, expected one paragraph: %+v", len(doc.Blocks), doc.Blocks)
	}
}

// Two places decide what opens a block: the parser's own dispatch and
// isBlockStart, which ends the paragraph or list item in progress. A rule added
// to one and not the other swallows the block into the text above it, which is
// how the table case was found. One line per block kind holds them in step.
func TestEveryBlockKindEndsTheParagraphAboveIt(t *testing.T) {
	cases := []struct {
		kind  string
		text  string
		emits bool // a horizontal rule is consumed and leaves no block behind
	}{
		{"heading", "# Heading", true},
		{"rule", "---", false},
		{"fence", "```\ncode\n```", true},
		{"image", "![Caption](a.png)", true},
		{"quote", "> quoted", true},
		{"bullet", "- item", true},
		{"ordered", "1. item", true},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |", true},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			doc := Parse("Texte au-dessus.\n" + c.text + "\n")
			para, ok := doc.Blocks[0].(Paragraph)
			if !ok {
				t.Fatalf("first block = %T, expected the paragraph", doc.Blocks[0])
			}
			if got := Plain(para.Inlines); got != "Texte au-dessus." {
				t.Errorf("paragraph = %q, it should stop where the %s begins", got, c.kind)
			}
			if want := 1; c.emits && len(doc.Blocks) < want+1 {
				t.Errorf("blocks = %+v, the %s produced none of its own", doc.Blocks, c.kind)
			}
		})
	}
}

// A front-matter value is data, not Markdown. Substituting it before the body was
// cut into blocks let a value that began with a marker open a block of its own.
func TestAVariableCannotOpenABlock(t *testing.T) {
	doc := Parse("---\nreference: IR-A-1\n" +
		"note: \"# Injected\"\nname: \"- Smith\"\nsep: \"a | b\"\nend: \"---\"\n---\n\n" +
		"{{note}}\n\n{{name}}\n\n{{sep}}\n\n{{end}}\n")

	if len(doc.Blocks) != 4 {
		t.Fatalf("blocks = %+v, expected four paragraphs", doc.Blocks)
	}
	for i, want := range []string{"# Injected", "- Smith", "a | b", "---"} {
		para, ok := doc.Blocks[i].(Paragraph)
		if !ok {
			t.Fatalf("block %d = %T, expected a paragraph carrying the value verbatim", i, doc.Blocks[i])
		}
		if got := Plain(para.Inlines); got != want {
			t.Errorf("block %d = %q, expected %q", i, got, want)
		}
	}
}

// Expansion has to reach every place text can stand, since it no longer runs
// over the source that all of them came from.
func TestVariablesExpandEverywhereTextStands(t *testing.T) {
	doc := Parse("---\nreference: IR-A-1\nv: Lucie\n---\n\n" +
		"# Heading {{v}}\n\nText {{v}}.\n\n> Quoted {{v}}\n\n- Bullet {{v}}\n\n1. Number {{v}}\n\n" +
		"```\ncode {{v}}\n```\n\n![Caption {{v}}]({{v}}.png)\n\n| a {{v}} | b |\n|---|---|\n| c | d {{v}} |\n\n" +
		"A [link {{v}}](https://example.org/{{v}}) here.\n")

	for i, b := range doc.Blocks {
		var got string
		switch v := b.(type) {
		case Heading:
			got = Plain(v.Inlines)
		case Paragraph:
			got = Plain(v.Inlines)
		case Quote:
			got = Plain(v.Inlines)
		case Bullet:
			got = Plain(v.Inlines)
		case Ordered:
			got = Plain(v.Inlines)
		case Code:
			got = v.Text
		case Image:
			got = v.Path + " " + Plain(v.Caption)
		case Table:
			for _, row := range v.Rows {
				for _, cell := range row {
					got += Plain(cell)
				}
			}
		}
		if strings.Contains(got, "{{") {
			t.Errorf("block %d (%T) = %q, a variable was left unexpanded", i, b, got)
		}
		if !strings.Contains(got, "Lucie") {
			t.Errorf("block %d (%T) = %q, expected the value in it", i, b, got)
		}
	}

	// A link carries text and a destination, and both hold variables.
	last := doc.Blocks[len(doc.Blocks)-1].(Paragraph)
	var href string
	for _, in := range last.Inlines {
		if in.Href != "" {
			href = in.Href
		}
	}
	if href != "https://example.org/Lucie" {
		t.Errorf("href = %q, a destination must expand too", href)
	}
}

// A name caught in a cycle has no value to stand for it, in the body as much as
// in the front matter, so what the body wrote is what the reader sees.
func TestACycleStaysAsWrittenInTheBody(t *testing.T) {
	doc := Parse("---\nreference: IR-A-1\ntic: \"{{tac}}\"\ntac: \"{{tic}}\"\n---\n\nVoir {{tic}}.\n")
	if got := Plain(doc.Blocks[0].(Paragraph).Inlines); got != "Voir {{tic}}." {
		t.Errorf("paragraph = %q, the body wrote {{tic}} and should still show it", got)
	}
}

// Values are inserted as text: a value is no longer able to bring formatting of
// its own into the document.
func TestAVariableValueIsNotMarkdown(t *testing.T) {
	doc := Parse("---\nreference: IR-A-1\nname: \"**Smith**\"\n---\n\nHello {{name}}.\n")
	inls := doc.Blocks[0].(Paragraph).Inlines
	if got := Plain(inls); got != "Hello **Smith**." {
		t.Errorf("paragraph = %q, the value should read verbatim", got)
	}
	for _, in := range inls {
		if in.Bold {
			t.Errorf("run %q came out bold: a value should not carry formatting", in.Text)
		}
	}
}

// A tab is two columns wide but a single byte long. Counting the width as if it
// were the offset walked off the end of a short line, and left a tab-indented
// list looking like a paragraph.
func TestParseListItemIndentedWithTabs(t *testing.T) {
	cases := []struct {
		line   string
		indent int
		rest   string
		ok     bool
	}{
		{"- top", 0, "top", true},
		{"\t- once", 2, "once", true},
		{"\t\t- twice", 4, "twice", true},
		{"\t  - mixed", 4, "mixed", true},
		{"\t\t1. numbered", 4, "numbered", true},
		{"\t\tX", 0, "", false}, // no marker: not a list item, and no panic
	}
	for _, c := range cases {
		indent, _, rest, ok := parseListItem(c.line)
		if ok != c.ok || rest != c.rest || (ok && indent != c.indent) {
			t.Errorf("parseListItem(%q) = (%d, %q, %v), expected (%d, %q, %v)",
				c.line, indent, rest, ok, c.indent, c.rest, c.ok)
		}
	}
}

func TestParseBlocksNestsTabIndentedLists(t *testing.T) {
	blocks := parseBlocks("- top\n\t- nested\n\t\t- deeper\n")
	var levels []int
	for _, b := range blocks {
		bullet, ok := b.(Bullet)
		if !ok {
			t.Fatalf("block %T, expected a Bullet", b)
		}
		levels = append(levels, bullet.Level)
	}
	if want := []int{0, 1, 2}; fmt.Sprint(levels) != fmt.Sprint(want) {
		t.Errorf("levels = %v, expected %v", levels, want)
	}
}

// A line joins the paragraph above it unless it carries a marker of its own. A
// hash or a chevron is not a marker by itself: "#1" is a rank and ">=" is a
// comparison, and an analyst writes both in the middle of a sentence.
func TestAHashOrChevronThatIsNoMarkerJoinsTheParagraph(t *testing.T) {
	blocks := parseBlocks("The finding is\n#1 by severity and applies where\n>= 2 hosts are affected.\n")
	if len(blocks) != 1 {
		t.Fatalf("%d blocks, expected the three lines to make one paragraph", len(blocks))
	}
	want := "The finding is #1 by severity and applies where >= 2 hosts are affected."
	if got := Plain(blocks[0].(Paragraph).Inlines); got != want {
		t.Errorf("paragraph = %q, expected %q", got, want)
	}
}
