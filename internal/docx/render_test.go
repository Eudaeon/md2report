package docx

import (
	"fmt"
	"md2report/internal/markdown"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// fakeResources is the second implementation of docResources. It records what it
// was asked for and hands back recognisable ids, with no .docx in sight.
type fakeResources struct {
	images []string // extension of each image stored
	mimes  []string
	links  []string
	nums   int
}

func (f *fakeResources) addImage(data []byte, ext, mime string) string {
	f.images = append(f.images, ext)
	f.mimes = append(f.mimes, mime)
	return fmt.Sprintf("rIdImage%d", len(f.images))
}

func (f *fakeResources) addLink(url string) string {
	f.links = append(f.links, url)
	return fmt.Sprintf("rIdLink%d", len(f.links))
}

func (f *fakeResources) addNumbering() int {
	f.nums++
	return 900 + f.nums
}

// renderBody produces a document body with no template, in a 9072 × 14004 twip
// page.
func renderBody(t *testing.T, baseDir string, blocks ...markdown.Block) (string, []tocEntry, *fakeResources) {
	t.Helper()
	res := &fakeResources{}
	body, toc, err := newRenderer(res, templateStyles(), baseDir, 9072, 14004).body(blocks)
	if err != nil {
		t.Fatal(err)
	}
	return body, toc, res
}

func TestRenderStyles(t *testing.T) {
	body, toc, _ := renderBody(t, "",
		markdown.Heading{Level: 1, Inlines: []markdown.Inline{{Text: "Summary"}}},
		markdown.Heading{Level: 2, Inlines: []markdown.Inline{{Text: "Detail"}}},
		markdown.Paragraph{Inlines: []markdown.Inline{{Text: "Some text."}}},
		markdown.Bullet{Inlines: []markdown.Inline{{Text: "a bullet"}}},
		markdown.Quote{Inlines: []markdown.Inline{{Text: "a quotation"}}},
		markdown.Code{Text: "go build"},
	)
	for _, want := range []string{
		`<w:pStyle w:val="` + styPart + `"/>`,
		`<w:pStyle w:val="` + styHeading(2) + `"/>`,
		`<w:pStyle w:val="` + styBody + `"/>`,
		`<w:pStyle w:val="` + styBullet + `"/>`,
		`<w:pStyle w:val="` + styQuote + `"/>`,
		`Consolas`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %s", want)
		}
	}
	if len(toc) != 1 || toc[0].text != "Summary" {
		t.Fatalf("table of contents = %+v, only one level-1 heading expected", toc)
	}
	if !strings.Contains(body, `w:name="`+toc[0].bookmark+`"`) {
		t.Errorf("entry %s sets no bookmark in the body", toc[0].bookmark)
	}
}

// Each ordered list gets its own numbering; items of the same list share it.
func TestRenderOrderedLists(t *testing.T) {
	body, _, res := renderBody(t, "",
		markdown.Ordered{ListID: 1, Inlines: []markdown.Inline{{Text: "one"}}},
		markdown.Ordered{ListID: 1, Inlines: []markdown.Inline{{Text: "two"}}},
		markdown.Ordered{ListID: 2, Inlines: []markdown.Inline{{Text: "another list"}}},
	)
	if res.nums != 2 {
		t.Errorf("%d numbering(s) requested, 2 expected", res.nums)
	}
	ids := regexp.MustCompile(`<w:numId w:val="(\d+)"/>`).FindAllStringSubmatch(body, -1)
	if len(ids) != 3 {
		t.Fatalf("%d numbered item(s), 3 expected", len(ids))
	}
	if ids[0][1] != ids[1][1] {
		t.Errorf("items of one list should share a numId (%s ≠ %s)", ids[0][1], ids[1][1])
	}
	if ids[1][1] == ids[2][1] {
		t.Errorf("two distinct lists share numId %s", ids[2][1])
	}
}

func TestRenderLinks(t *testing.T) {
	body, _, res := renderBody(t, "", markdown.Paragraph{Inlines: []markdown.Inline{
		{Text: "write to "},
		{Text: "the victim", Href: "mailto:a.martin@example.com"},
	}})
	if len(res.links) != 1 || res.links[0] != "mailto:a.martin@example.com" {
		t.Fatalf("links declared = %v", res.links)
	}
	if !strings.Contains(body, `<w:hyperlink r:id="rIdLink1"`) {
		t.Error("the link does not point at the relationship the registry handed back")
	}
	if !strings.Contains(body, `<w:rStyle w:val="`+styLink+`"/>`) {
		t.Error("the link character style is missing")
	}
}

func TestRenderInlineFormatting(t *testing.T) {
	body, _, _ := renderBody(t, "", markdown.Paragraph{Inlines: []markdown.Inline{
		{Text: "bold", Bold: true},
		{Text: "italic", Italic: true},
		{Text: "code", Code: true},
		{Text: "5 > 3 & <tag>"},
	}})
	for _, want := range []string{`<w:b/>`, `<w:i/>`, `Consolas`, `5 &gt; 3 &amp; &lt;tag&gt;`} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %s", want)
		}
	}
}

// The width asked for applies to the usable page area; the height follows at
// constant proportions.
func TestRenderImages(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "screenshot.png"), 400, 200)

	body, _, res := renderBody(t, dir,
		markdown.Image{Path: "screenshot.png", Width: markdown.ImageWidth{Value: 50, Unit: "%"}, Caption: []markdown.Inline{{Text: "Connections"}}},
	)
	if len(res.images) != 1 || res.images[0] != "png" || res.mimes[0] != "image/png" {
		t.Fatalf("images stored = %v (%v)", res.images, res.mimes)
	}
	if !strings.Contains(body, `r:embed="rIdImage1"`) {
		t.Error("the drawing does not point at the relationship the registry handed back")
	}

	m := regexp.MustCompile(`<wp:extent cx="(\d+)" cy="(\d+)"/>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no image extent in the body")
	}
	wantCx := 9072 * emuPerTwip / 2
	if m[1] != strconv.Itoa(wantCx) {
		t.Errorf("width = %s EMU, expected %d (half the usable area)", m[1], wantCx)
	}
	if m[2] != strconv.Itoa(wantCx/2) {
		t.Errorf("height = %s EMU, expected %d (400×200, proportions kept)", m[2], wantCx/2)
	}
	if !strings.Contains(body, `<w:pStyle w:val="`+styFigureCaption+`"/>`) || !strings.Contains(body, "Figure 1") {
		t.Error("the numbered caption is missing")
	}
}

// An image wider than the page is shrunk; a smaller one keeps its native size.
func TestRenderImageFitting(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "huge.png"), 4000, 200)
	writePNG(t, filepath.Join(dir, "thumb.png"), 20, 10)

	body, _, _ := renderBody(t, dir, markdown.Image{Path: "huge.png"}, markdown.Image{Path: "thumb.png"})
	ext := regexp.MustCompile(`<wp:extent cx="(\d+)"`).FindAllStringSubmatch(body, -1)
	if len(ext) != 2 {
		t.Fatalf("%d image(s), 2 expected", len(ext))
	}
	maxW := 9072 * emuPerTwip
	if got, _ := strconv.Atoi(ext[0][1]); got != maxW {
		t.Errorf("an over-wide image should come back to %d EMU, got %d", maxW, got)
	}
	if got, _ := strconv.Atoi(ext[1][1]); got != 20*emuPerPx {
		t.Errorf("a small image keeps its native size (%d EMU), got %d", 20*emuPerPx, got)
	}
}

func TestRenderMissingImage(t *testing.T) {
	res := &fakeResources{}
	_, _, err := newRenderer(res, templateStyles(), t.TempDir(), 9072, 14004).body([]markdown.Block{markdown.Image{Path: "missing.png"}})
	if err == nil || !strings.Contains(err.Error(), "missing.png") {
		t.Fatalf("an error was expected for a missing image, got: %v", err)
	}
}

func TestRenderTables(t *testing.T) {
	cell := func(s string) []markdown.Inline { return []markdown.Inline{{Text: s}} }
	body, _, _ := renderBody(t, "", markdown.Table{Rows: [][][]markdown.Inline{
		{cell("Date"), cell("Fait")},
		{cell("25/07"), cell("Connexion")},
	}})
	if !strings.Contains(body, "<w:tbl>") || strings.Count(body, "<w:tr>") != 2 {
		t.Errorf("malformed table: %s", body)
	}
	if !strings.Contains(body, "<w:tblHeader/>") || !strings.Contains(body, `w:fill="F2F2F2"`) {
		t.Error("the first row should be a shaded header")
	}
	// Two columns share the usable area.
	if !strings.Contains(body, `<w:gridCol w:w="4536"/>`) {
		t.Errorf("unexpected column width in %s", body)
	}
}

// A TOC field that opens must close, even with no entry at all.
func TestTOCField(t *testing.T) {
	for _, entries := range [][]tocEntry{nil, {{text: "Summary", bookmark: "_TocMd1"}}} {
		xml := tocField(entries, templateStyles())
		if a, b := strings.Count(xml, `fldCharType="begin"`), strings.Count(xml, `fldCharType="end"`); a != b {
			t.Errorf("%d entry/entries: %d field start(s) for %d end(s)", len(entries), a, b)
		}
	}
}

// Bullets take the numbering the template's own style names, not a fixed one.
func TestBulletsUseTheTemplatesNumbering(t *testing.T) {
	st := templateStyles()
	st.bulletNum = 7
	body, _, err := newRenderer(&fakeResources{}, st, t.TempDir(), 9072, 14004).body([]markdown.Block{
		markdown.Bullet{Inlines: []markdown.Inline{{Text: "un"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<w:numId w:val="7"/>`) {
		t.Errorf("bullet = %s, expected the numbering the style names", body)
	}

	// A style naming none leaves the paragraph to inherit whatever it carries.
	body, _, err = newRenderer(&fakeResources{}, templateStyles(), t.TempDir(), 9072, 14004).body([]markdown.Block{
		markdown.Bullet{Inlines: []markdown.Inline{{Text: "un"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<w:numPr>") {
		t.Errorf("bullet = %s, expected no numbering of its own", body)
	}
}

// A percentage is a fraction of the page, and the fraction may itself be
// fractional. Rounding the percentage down before applying it loses half a
// point of width for every half percent asked.
func TestAPercentageWidthKeepsItsFraction(t *testing.T) {
	const page = 5000000
	cases := []struct {
		w    markdown.ImageWidth
		want int
	}{
		{markdown.ImageWidth{Value: 62.5, Unit: "%"}, page * 625 / 1000},
		{markdown.ImageWidth{Value: 60, Unit: "%"}, page * 60 / 100},
		{markdown.ImageWidth{Value: 8.5, Unit: "cm"}, 3060000},
		{markdown.ImageWidth{}, 0},
	}
	for _, c := range cases {
		if got := targetWidth(c.w, page); got != c.want {
			t.Errorf("targetWidth(%v) = %d, want %d", c.w, got, c.want)
		}
	}
}

// A report quotes what the incident left behind, and a terminal capture pasted
// into one carries control bytes no escaping can put into XML.
func TestEscDropsCharactersXMLCannotHold(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a<b>&\"c\"", "a&lt;b&gt;&amp;&quot;c&quot;"},
		{"\x1b[31mred\x1b[0m", "[31mred[0m"}, // ANSI escapes
		{"bell\x07 null\x00 vt\x0b", "bell null vt"},
		{"kept\ttab\nnewline", "kept\ttab\nnewline"}, // legal, and runText needs them
		{"café 中文", "café 中文"},
		{"bad\xffbyte", "bad�byte"}, // invalid UTF-8 becomes the replacement rune
	}
	for _, c := range cases {
		if got := esc(c.in); got != c.want {
			t.Errorf("esc(%q) = %q, expected %q", c.in, got, c.want)
		}
	}
}

func TestLinkableOnlyAllowsWhatWordMaySafelyFollow(t *testing.T) {
	for _, href := range []string{"https://example.com", "http://example.com", "HTTPS://EXAMPLE.COM", "mailto:x@y.z"} {
		if !linkable(href) {
			t.Errorf("linkable(%q) = false, expected true", href)
		}
	}
	for _, href := range []string{
		`\\host\share\x`,      // UNC: hands over the reader's credentials
		"file:///etc/passwd",  //
		"javascript:alert(1)", //
		"//example.com/x",     // protocol-relative, resolves to a UNC path in Word
		"", "ftp://example.com",
	} {
		if linkable(href) {
			t.Errorf("linkable(%q) = true, expected false", href)
		}
	}
}

// A destination Word must not follow keeps its text and loses only the click.
func TestRunsLeaveAnUnsafeLinkAsText(t *testing.T) {
	res := &fakeResources{}
	r := newRenderer(res, templateStyles(), "", 8064, 11664)
	got := r.runs([]markdown.Inline{{Text: "payload", Href: `\\evil.example.com\share`}})
	if strings.Contains(got, "w:hyperlink") {
		t.Errorf("runs() = %q, expected no hyperlink", got)
	}
	if !strings.Contains(got, "payload") {
		t.Errorf("runs() = %q, expected the text to survive", got)
	}
	if len(res.links) != 0 {
		t.Errorf("links = %v, expected none declared", res.links)
	}
}

// A width in the millions of inches overflows the conversion to EMU, and the
// drawing that comes out is sized by whatever the arithmetic wrapped to.
func TestTargetWidthStopsAtThePage(t *testing.T) {
	const maxW = 5120640
	cases := []struct {
		w    markdown.ImageWidth
		want int
	}{
		{markdown.ImageWidth{}, 0}, // no width asked
		{markdown.ImageWidth{Value: 50, Unit: "%"}, maxW / 2},
		{markdown.ImageWidth{Value: 1, Unit: "in"}, 914400},
		{markdown.ImageWidth{Value: 1e30, Unit: "in"}, maxW},
		{markdown.ImageWidth{Value: 99999999, Unit: "in"}, maxW},
		{markdown.ImageWidth{Value: 500, Unit: "%"}, maxW},
	}
	for _, c := range cases {
		if got := targetWidth(c.w, maxW); got != c.want {
			t.Errorf("targetWidth(%v) = %d, expected %d", c.w, got, c.want)
		}
	}
}

// The template decides what introduces a caption; the renderer only counts the
// figures and puts the number where the format asks for it.
func TestRenderCaptionUsesTheTemplateFormat(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "one.png"), 40, 20)

	st := templateStyles()
	st.caption = "Figure {n} & suite : "
	body, _, err := newRenderer(&fakeResources{}, st, dir, 9072, 14004).body([]markdown.Block{
		markdown.Image{Path: "one.png", Caption: []markdown.Inline{{Text: "first"}}},
		markdown.Image{Path: "one.png", Caption: []markdown.Inline{{Text: "second"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Figure 1 &amp; suite : ", "Figure 2 &amp; suite : "} {
		if !strings.Contains(body, want) {
			t.Errorf("caption %q missing: the template's format should carry the figure number, escaped for XML", want)
		}
	}
	if strings.Contains(body, "Figure 1:") {
		t.Error("the default format was used even though the template declared one")
	}
}
