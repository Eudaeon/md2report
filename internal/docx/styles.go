package docx

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The styles md2report writes, named in English because md2report names them.
// The template supplies the style sheet, and the two meet on these names.
//
// Word localizes the styleId of its own styles: "heading 2" is Titre2 in a
// French Word and Überschrift2 in a German one. It keeps the English name in
// <w:name> either way, so a role matches on either spelling and a built-in
// style is found whatever language the template was authored in. A style the
// template author invented carries no such English name, and a template that
// spells one differently declares the styleId it uses.
const (
	styPart          = "Part"
	styBody          = "Body"
	styBullet        = "Bullet"
	styQuote         = "Quote"
	styImage         = "Image"
	styFigureCaption = "FigureCaption"
	styContents      = "Contents"
	styLink          = "Link"
	styHyperlink     = "Hyperlink"
	styTable         = "NormalTable"
)

// styHeading names the style of a heading below level 1. Level 1 is styPart: a
// report's top-level headings are its parts, and they alone reach the table of
// contents.
func styHeading(level int) string { return "Heading" + strconv.Itoa(level) }

// roles is every style a report can ask for, against the kind of style it has
// to be. The kind is checked because a role matches on a name, and nothing
// stops a template from giving a character style the name of a paragraph one.
var roles = func() map[string]string {
	r := map[string]string{
		styPart:          "paragraph",
		styBody:          "paragraph",
		styBullet:        "paragraph",
		styQuote:         "paragraph",
		styImage:         "paragraph",
		styFigureCaption: "paragraph",
		styContents:      "paragraph",
		styLink:          "character",
		styHyperlink:     "character",
		styTable:         "table",
	}
	// Markdown stops at six hashes, and the first level is a part.
	for level := 2; level <= 6; level++ {
		r[styHeading(level)] = "paragraph"
	}
	return r
}()

var (
	reStyleDefn = regexp.MustCompile(`(?s)<w:style ([^>]*)>(.*?)</w:style>`)
	reStyleAtID = regexp.MustCompile(`w:styleId="([^"]*)"`)
	reStyleAtTy = regexp.MustCompile(`w:type="([^"]*)"`)
	reStyleName = regexp.MustCompile(`<w:name w:val="([^"]*)"`)

	// reStyleProp reads a declared style out of the custom document properties,
	// where a template that spells a style its own way says so.
	reStyleProp = regexp.MustCompile(`(?s)<property[^>]*name="md2report:style:([^"]+)"[^>]*>\s*<vt:lpwstr>(.*?)</vt:lpwstr>`)
)

// styleSet is what one template says about house style: the name it spells each
// role with, the numbering its bullet style already carries, and the text it
// introduces a caption with. All of it is read from the template once, and the
// rest of the package asks this rather than the .docx.
//
// It records the roles a report actually asked for, so that a template missing a
// style is only a problem once a report wants it: a template defining no table
// style is nobody's business until a report has a table.
type styleSet struct {
	id   map[string]string
	used map[string]bool

	caption   string // what goes before an image's caption, {n} for the number
	bulletNum int    // the numbering the bullet style names, 0 for none
}

// get names the style this template uses for a role, and notes that the report
// asked for it. It gives back an empty name for a role the template leaves
// unresolved, and unresolved reports it afterwards.
func (s *styleSet) get(role string) string {
	s.used[role] = true
	return s.id[role]
}

// unresolved names the roles a report used and its template never defined.
func (s *styleSet) unresolved() []string {
	var missing []string
	for role := range s.used {
		if s.id[role] == "" {
			missing = append(missing, role)
		}
	}
	sort.Strings(missing)
	return missing
}

// resolveStyles matches every role against a template's style sheet. A style
// declared in the custom document properties wins, then a styleId spelled
// exactly as the role, then a styleId or an English <w:name> that differs only
// in case and punctuation, which is what makes "Heading2" find "heading 2" and
// "NormalTable" find "Normal Table".
func resolveStyles(stylesXML, customXML []byte) *styleSet {
	type definition struct{ id, name, typ string }
	var defined []definition
	for _, m := range reStyleDefn.FindAllSubmatch(stylesXML, -1) {
		attrs, inner := string(m[1]), string(m[2])
		defined = append(defined, definition{
			id:   submatch(reStyleAtID, attrs),
			typ:  submatch(reStyleAtTy, attrs),
			name: submatch(reStyleName, inner),
		})
	}

	declared := map[string]string{}
	for _, m := range reStyleProp.FindAllSubmatch(customXML, -1) {
		declared[string(m[1])] = string(m[2])
	}

	set := &styleSet{id: map[string]string{}, used: map[string]bool{}}
	for role, typ := range roles {
		want := normalizeStyle(role)
		var byID, byName string
		for _, d := range defined {
			if d.typ != typ {
				continue
			}
			if name, ok := declared[role]; ok {
				if d.id == name {
					byID = d.id
					break
				}
				continue
			}
			if d.id == role {
				byID = d.id
				break
			}
			if byID == "" && normalizeStyle(d.id) == want {
				byID = d.id
			}
			if byName == "" && normalizeStyle(d.name) == want {
				byName = d.id
			}
		}
		if byID != "" {
			set.id[role] = byID
		} else if byName != "" {
			set.id[role] = byName
		}
	}
	set.caption = captionFormat(customXML)
	set.bulletNum = bulletNumbering(stylesXML, set.id[styBullet])
	return set
}

// captionDefault is what goes before an image's caption when the template asks
// for nothing else.
const captionDefault = "Figure {n}: "

// reCaption reads the caption format out of the custom document properties.
var reCaption = regexp.MustCompile(`(?s)<property[^>]*name="md2report:caption"[^>]*>\s*<vt:lpwstr>(.*?)</vt:lpwstr>`)

// captionFormat is the text put before an image's caption, {n} standing for the
// figure number. A template declares it as the custom document property
// md2report:caption and gets the default when it declares none.
//
// It belongs to the template rather than to a report or a command line for the
// same reason the style names do: it is house style, the same in every report a
// template produces, and a template that introduces its figures otherwise should
// say so once instead of in the front matter of every report anyone ever writes.
func captionFormat(customXML []byte) string {
	m := reCaption.FindSubmatch(customXML)
	if m == nil {
		return captionDefault
	}
	return html.UnescapeString(string(m[1]))
}

var reNumID = regexp.MustCompile(`<w:numId w:val="(\d+)"/>`)

// bulletNumbering reports the numbering the template's bullet style uses. The
// style carries its own <w:numPr>, and copying a fixed id instead would give the
// bullets of another template whatever list happened to hold that number.
//
// The search stops at the end of that one style, so the numbering of the next
// style along cannot be mistaken for it.
func bulletNumbering(stylesXML []byte, style string) int {
	if style == "" {
		return 0
	}
	oneStyle := regexp.MustCompile(`(?s)<w:style [^>]*w:styleId="` + regexp.QuoteMeta(style) + `".*?</w:style>`)
	m := oneStyle.Find(stylesXML)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(submatch(reNumID, string(m)))
	if err != nil {
		return 0
	}
	return n
}

// normalizeStyle strips a style name down to what two spellings of the same
// style share: case and punctuation are all that separate "Heading2" from the
// "heading 2" Word writes into <w:name>.
func normalizeStyle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
