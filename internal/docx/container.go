package docx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"md2report/internal/markdown"
)

// part is an entry of the .docx ZIP container.
type part struct {
	name     string
	data     []byte
	method   uint16
	modified time.Time
}

// builder holds the template in memory along with what rendering has asked for
// so far: relationships, media and numberings still to add. It implements
// docResources over a real .docx.
type builder struct {
	parts []part

	nextRel   int
	newRels   []string
	nextMedia int
	numIDs    []int             // numIds to create for the ordered lists
	links     map[string]string // URL -> relationship id
	media     map[string]string // image content -> relationship id
}

func openTemplate(path string) (*builder, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	b := &builder{nextRel: 1000, links: map[string]string{}, media: map[string]string{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		b.parts = append(b.parts, part{name: f.Name, data: data, method: f.Method, modified: f.Modified})
	}
	// Every package declares its parts' types, and every Word document has a
	// body. Without either there is nothing to fill, and the helpers below would
	// have nowhere to write.
	if b.get("word/document.xml") == nil || b.get("[Content_Types].xml") == nil {
		return nil, fmt.Errorf("%s is not a valid Word document", path)
	}
	return b, nil
}

func (b *builder) get(name string) []byte {
	for _, p := range b.parts {
		if p.name == name {
			return p.data
		}
	}
	return nil
}

func (b *builder) set(name string, data []byte) {
	for i := range b.parts {
		if b.parts[i].name == name {
			b.parts[i].data = data
			return
		}
	}
	b.parts = append(b.parts, part{name: name, data: data, method: zip.Deflate, modified: time.Now()})
}

func (b *builder) write(path string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range b.parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: p.method, Modified: p.modified})
		if err != nil {
			return err
		}
		if _, err := w.Write(p.data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	// A report is confidential until its author says otherwise, so it is not
	// readable by everyone with an account on the machine that built it. An
	// earlier run's file is narrowed too, since WriteFile only sets the mode of a
	// file it creates.
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// finish writes into the template everything rendering the body asked the
// container for. The steps run in one order and no other: a rendered body has
// left relationships and numberings to declare, turning the fields on may add a
// relationship of its own, and pruning can only tell what nothing refers to once
// every reference is in place. Keeping the sequence here is what stops a caller
// from having to know it.
func (b *builder) finish(m markdown.Meta, templateRef string) error {
	b.applyReference(templateRef, m.Reference)
	b.applyDocProps(m)
	b.applyUpdateFields()
	if err := b.applyRels(); err != nil {
		return err
	}
	if err := b.applyNumbering(); err != nil {
		return err
	}
	b.prune()
	return nil
}

// addImage puts an image into word/media, declares the relationship pointing at
// it, and makes sure the container knows the extension. Like a link, one image
// yields only one part however often the report shows it: a diagram repeated in
// every section would otherwise be carried once per mention, and screenshots are
// the bulk of what a report weighs.
func (b *builder) addImage(data []byte, ext, mime string) string {
	sum := sha256.Sum256(data)
	key := string(sum[:])
	if id, ok := b.media[key]; ok {
		return id
	}
	b.ensureExtension(ext, mime)
	b.nextMedia++
	name := fmt.Sprintf("media/md-image-%d.%s", b.nextMedia, ext)
	b.parts = append(b.parts, part{name: "word/" + name, data: data, method: zip.Deflate, modified: time.Now()})
	id := b.addRel("image", name, "")
	b.media[key] = id
	return id
}

// addLink declares an external link; one URL yields only one relationship.
func (b *builder) addLink(url string) string {
	if id, ok := b.links[url]; ok {
		return id
	}
	id := b.addRel("hyperlink", url, "External")
	b.links[url] = id
	return id
}

// addNumbering reserves a numId; applyNumbering defines them all at once.
func (b *builder) addNumbering() int {
	id := 900 + len(b.numIDs) + 1
	b.numIDs = append(b.numIDs, id)
	return id
}

// addRel records a new relationship in word/_rels/document.xml.rels and returns
// its id.
func (b *builder) addRel(relType, target, mode string) string {
	b.nextRel++
	id := "rId" + strconv.Itoa(b.nextRel)
	rel := fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s"`,
		id, relType, esc(target))
	if mode != "" {
		rel += fmt.Sprintf(` TargetMode="%s"`, mode)
	}
	b.newRels = append(b.newRels, rel+"/>")
	return id
}

func (b *builder) applyRels() error {
	if len(b.newRels) == 0 {
		return nil
	}
	const name = "word/_rels/document.xml.rels"
	s := string(b.get(name))
	if s == "" {
		return fmt.Errorf("the template has no %s, and the images and links of the report have nowhere to point", name)
	}
	s = strings.Replace(s, "</Relationships>", strings.Join(b.newRels, "")+"</Relationships>", 1)
	b.set(name, []byte(s))
	return nil
}

// reUpdateFields spots the setting whether it stands alone or wraps a closing
// tag, so that replacing it leaves no orphan </w:updateFields> behind.
var reUpdateFields = regexp.MustCompile(`<w:updateFields[^>]*>(\s*</w:updateFields>)?`)

// applyUpdateFields asks Word to recompute the fields when the document opens.
// That recomputation is what fills in the table-of-contents page numbers. A
// template that already carries the setting has it overwritten, since one turned
// off would leave the page numbers at whatever the template last held.
func (b *builder) applyUpdateFields() {
	const name = "word/settings.xml"
	const tag = `<w:updateFields w:val="true"/>`
	s := string(b.get(name))
	if s == "" {
		b.addSettings(tag)
		return
	}
	if reUpdateFields.MatchString(s) {
		b.set(name, []byte(reUpdateFields.ReplaceAllString(s, tag)))
		return
	}
	for _, anchor := range []string{"<w:hdrShapeDefaults>", "<w:footnotePr>", "<w:compat>", "</w:settings>"} {
		if strings.Contains(s, anchor) {
			s = strings.Replace(s, anchor, tag+anchor, 1)
			break
		}
	}
	b.set(name, []byte(s))
}

// applyNumbering defines one decimal numbering per ordered list, each restarted
// at 1.
func (b *builder) applyNumbering() error {
	if len(b.numIDs) == 0 {
		return nil
	}
	const name = "word/numbering.xml"
	s := string(b.get(name))
	if s == "" {
		// A template that never held a numbered list has no numbering part, and
		// nothing would define the numbers the body already points at.
		return fmt.Errorf("the template has no %s, so the numbered lists of the report would come out unnumbered", name)
	}
	const absID = 900
	var abs strings.Builder
	fmt.Fprintf(&abs, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, absID)
	// One level for each the parser admits, or a list nested deeper than the
	// numbering reaches would point at a level nothing defines.
	fmts := []string{"decimal", "lowerLetter", "lowerRoman", "decimal", "lowerLetter"}
	for lvl := range fmts {
		fmt.Fprintf(&abs, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%%%d."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="357"/></w:pPr></w:lvl>`,
			lvl, fmts[lvl], lvl+1, 714+lvl*357)
	}
	abs.WriteString(`</w:abstractNum>`)

	var nums strings.Builder
	for _, id := range b.numIDs {
		fmt.Fprintf(&nums, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, id, absID)
	}

	if i := strings.Index(s, "<w:num "); i >= 0 {
		s = s[:i] + abs.String() + s[i:]
	} else {
		s = strings.Replace(s, "</w:numbering>", abs.String()+"</w:numbering>", 1)
	}
	s = strings.Replace(s, "</w:numbering>", nums.String()+"</w:numbering>", 1)
	b.set(name, []byte(s))
	return nil
}

// A reference is read a paragraph at a time. Word is free to split text over
// several runs, and does so as soon as somebody edits a placeholder in the
// middle, so a reference reaching applyReference as "MODELE" plus "-000" is the
// ordinary case rather than the odd one.
var (
	reParagraph = regexp.MustCompile(`(?s)<w:p[ >].*?</w:p>`)
	reRunText   = regexp.MustCompile(`<w:t(?: [^>]*)?>([^<]*)</w:t>`)
)

// paraText is what a paragraph says, the runs it is split over joined back
// together.
func paraText(raw []byte) string {
	var text []byte
	for _, m := range reRunText.FindAllSubmatch(raw, -1) {
		text = append(text, m[1]...)
	}
	return string(bytes.TrimSpace(text))
}

// applyReference rewrites the incident reference wherever the template repeats it
// outside the body. The headers and footers carry it in a text box, and Word
// stores such a box twice, once per drawing dialect, so both copies are rewritten.
//
// What to look for is the reference the template prints on its own cover page:
// a reference has no form md2report knows, and the template is the only thing
// that can say which words in its footer are one.
func (b *builder) applyReference(templateRef, ref string) {
	if templateRef == "" || ref == "" {
		return
	}
	for i, p := range b.parts {
		base := path.Base(p.name)
		if !strings.HasPrefix(p.name, "word/") {
			continue
		}
		if !strings.HasPrefix(base, "header") && !strings.HasPrefix(base, "footer") {
			continue
		}
		b.parts[i].data = reParagraph.ReplaceAllFunc(p.data, func(para []byte) []byte {
			return replaceReference(para, templateRef, ref)
		})
	}
}

// replaceReference rewrites the reference of one paragraph, or gives the
// paragraph back as it was. The reference has to fill the paragraph, which is
// what keeps a sentence that merely cites an incident ("... following incident
// MODELE-000, ...") from being overwritten. The whole reference goes back into
// the first run and the others are emptied: however many pieces the placeholder
// arrived in, it is one reference and it leaves as one run.
func replaceReference(para []byte, templateRef, ref string) []byte {
	runs := reRunText.FindAllSubmatchIndex(para, -1)
	if runs == nil || paraText(para) != templateRef {
		return para
	}
	var out bytes.Buffer
	end := 0
	for n, r := range runs {
		out.Write(para[end:r[2]])
		if n == 0 {
			out.WriteString(esc(ref))
		}
		end = r[3]
	}
	out.Write(para[end:])
	return out.Bytes()
}

// replText prepares a value for a regexp replacement template: XML-escaped, and
// with $ doubled so that it is not read as a group reference.
func replText(s string) string { return strings.ReplaceAll(esc(s), "$", "$$") }

// setXMLText replaces the text of a simple element and keeps its attributes. An
// element the part does not carry is left alone rather than invented: knowing
// where a missing one belongs means knowing the schema, and this tool fills one
// template, whose properties are all present.
func setXMLText(s, tag, val string) string {
	re := regexp.MustCompile(`(<` + tag + `(?: [^>]*)?>)[^<]*(</` + tag + `>)`)
	return re.ReplaceAllString(s, "${1}"+replText(val)+"${2}")
}

var reLastPrinted = regexp.MustCompile(`<cp:lastPrinted[^>]*>[^<]*</cp:lastPrinted>`)

// applyDocProps rewrites the properties Word lists under File > Properties. The
// template's describe the template: the revision it reached, the minutes someone
// spent writing it, the day it was last printed, the length of a body this report
// has just replaced. Copied through, every report would repeat them to whoever
// opens it.
//
// The author is the exception. A front matter naming one is taken at its word,
// and md2report writes it as both the creator and the last to modify: the file
// was produced on that analyst's behalf, and leaving the template's name in one
// of the two would contradict the other. A front matter naming none leaves the
// template's own author alone, because a report is a deliverable of the firm
// whose template it is, and a blank author would only make the file look as
// though it came from nowhere. The corollary is that a template's properties are
// worth keeping clean: a name left in one rides out on every report that does not
// name its own author.
func (b *builder) applyDocProps(m markdown.Meta) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	if s := string(b.get("docProps/core.xml")); s != "" {
		s = setXMLText(s, "dc:title", m.Title)
		s = setXMLText(s, "dc:subject", m.Reference)
		if m.Author != "" {
			s = setXMLText(s, "dc:creator", m.Author)
			s = setXMLText(s, "cp:lastModifiedBy", m.Author)
		}
		s = setXMLText(s, "cp:revision", "1")
		s = setXMLText(s, "dcterms:created", now)
		s = setXMLText(s, "dcterms:modified", now)
		s = reLastPrinted.ReplaceAllString(s, "")
		b.set("docProps/core.xml", []byte(s))
	}

	// These count the template's body, not the report's. Word recomputes them
	// the next time the document is saved.
	if s := string(b.get("docProps/app.xml")); s != "" {
		for _, tag := range []string{"TotalTime", "Pages", "Words", "Characters", "CharactersWithSpaces", "Lines", "Paragraphs"} {
			s = setXMLText(s, tag, "0")
		}
		b.set("docProps/app.xml", []byte(s))
	}
}

var (
	reRelRef   = regexp.MustCompile(`r:(?:id|embed)="([^"]+)"`)
	reRelation = regexp.MustCompile(`<Relationship [^>]*/>`)
	reAttrID   = regexp.MustCompile(`Id="([^"]+)"`)
	reAttrType = regexp.MustCompile(`Type="([^"]+)"`)
	reTarget   = regexp.MustCompile(`Target="([^"]+)"`)
)

// prune drops the template's images and links that nothing refers to any more,
// so the screenshots of the original example do not travel with every report.
func (b *builder) prune() {
	used := map[string]bool{}
	for _, m := range reRelRef.FindAllSubmatch(b.get("word/document.xml"), -1) {
		used[string(m[1])] = true
	}

	const rels = "word/_rels/document.xml.rels"
	s := reRelation.ReplaceAllStringFunc(string(b.get(rels)), func(rel string) string {
		id := submatch(reAttrID, rel)
		typ := submatch(reAttrType, rel)
		if used[id] || !strings.HasSuffix(typ, "/image") && !strings.HasSuffix(typ, "/hyperlink") {
			return rel
		}
		return ""
	})
	b.set(rels, []byte(s))

	// A media file survives as long as some relationship still targets it.
	targets := map[string]bool{}
	for _, p := range b.parts {
		if !strings.Contains(p.name, "_rels/") {
			continue
		}
		dir := path.Dir(path.Dir(p.name))
		for _, m := range reTarget.FindAllSubmatch(p.data, -1) {
			targets[path.Join(dir, string(m[1]))] = true
		}
	}
	kept := b.parts[:0]
	for _, p := range b.parts {
		if strings.HasPrefix(p.name, "word/media/") && !targets[p.name] {
			continue
		}
		kept = append(kept, p)
	}
	b.parts = kept
}

func submatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// addSettings creates the settings part, for the rare template that carries
// none, along with the content type and the relationship that make it count as
// part of the document.
func (b *builder) addSettings(body string) {
	const (
		name = "word/settings.xml"
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"
	)
	data := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		body + `</w:settings>`
	b.parts = append(b.parts, part{name: name, data: []byte(data), method: zip.Deflate, modified: time.Now()})
	b.ensureOverride("/"+name, mime)
	b.addRel("settings", "settings.xml", "")
}

// ensureOverride declares one part's MIME type in [Content_Types].xml.
func (b *builder) ensureOverride(partName, mime string) {
	const name = "[Content_Types].xml"
	s := string(b.get(name))
	if strings.Contains(s, `PartName="`+partName+`"`) {
		return
	}
	s = strings.Replace(s, "</Types>", fmt.Sprintf(`<Override PartName="%s" ContentType="%s"/></Types>`, partName, mime), 1)
	b.set(name, []byte(s))
}

// ensureExtension declares an extension's MIME type in [Content_Types].xml.
func (b *builder) ensureExtension(ext, mime string) {
	const name = "[Content_Types].xml"
	s := string(b.get(name))
	if strings.Contains(s, `Extension="`+ext+`"`) {
		return
	}
	s = strings.Replace(s, "<Default", fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/><Default`, ext, mime), 1)
	b.set(name, []byte(s))
}
