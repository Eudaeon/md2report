// Package docx fills a Word template from a parsed Markdown document. It
// rewrites the cover page, the table of contents and the body, and copies the
// rest of the .docx through as it was.
package docx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"md2report/internal/markdown"
)

// Generate fills tplPath with the Markdown at mdPath and writes the document
// next to it, named after the incident reference. It returns the path it wrote
// and everything worth a warning: variables the front matter never defined. The
// caller decides what to do about those, and gets them even when the rest
// fails.
func Generate(mdPath, tplPath string) (out string, warnings []string, err error) {
	src, err := os.ReadFile(mdPath)
	if err != nil {
		return "", nil, err
	}
	doc := markdown.Parse(string(src))
	for _, name := range doc.Unknown {
		warnings = append(warnings, fmt.Sprintf("variable {{%s}} left unresolved", name))
	}

	out, err = outputPath(mdPath, doc.Meta.Reference)
	if err != nil {
		return "", warnings, err
	}
	b, err := openTemplate(tplPath)
	if err != nil {
		return "", warnings, fmt.Errorf("reading template: %w", err)
	}
	if err := b.fill(doc, filepath.Dir(mdPath)); err != nil {
		return "", warnings, err
	}
	return out, warnings, b.write(out)
}

// fill rewrites each region the template marks and leaves every other part of it
// alone. Image paths resolve against baseDir, the Markdown's directory.
func (b *builder) fill(doc *markdown.Document, baseDir string) error {
	lay, err := mapTemplate(b.get("word/document.xml"))
	if err != nil {
		return err
	}
	if err := lay.checkCover(doc.Meta); err != nil {
		return err
	}
	st := resolveStyles(b.get("word/styles.xml"), b.get("docProps/custom.xml"))
	body, toc, err := newRenderer(b, st, baseDir, lay.width, lay.height).body(doc.Blocks)
	if err != nil {
		return err
	}
	// The styles a table of contents is written in are only asked for when the
	// template offers one. A marker says what a template offers, and the roles a
	// report never uses are not checked.
	var tocXML, contents string
	if lay.marks[tocHere] {
		tocXML = tocField(toc, st)
		contents = st.get(styContents)
	}

	// Word renders a paragraph in Normal when it cannot find its style, and
	// says nothing about it, so a report can come out stripped of every heading
	// and still look, to Word, entirely well. Refusing is the only way the
	// reader of a template finds out.
	if missing := st.unresolved(); len(missing) > 0 {
		return fmt.Errorf("the template defines no style for %s: name the style after the role, or declare the one it uses as the custom document property %s",
			strings.Join(missing, ", "), styleProps(missing))
	}
	b.set("word/document.xml", lay.splice(doc.Meta, tocXML, body, contents))
	return b.finish(doc.Meta, lay.reference())
}

// styleProps names the custom document properties a template declares to point
// md2report at the styles it spells its own way.
func styleProps(roles []string) string {
	props := make([]string, len(roles))
	for i, role := range roles {
		props[i] = "md2report:style:" + role
	}
	return strings.Join(props, ", ")
}

// outputPath names the document after the incident reference and puts it next to
// the Markdown file.
func outputPath(mdPath, reference string) (string, error) {
	name := safeFileName(reference)
	if name == "" {
		return "", fmt.Errorf(`no reference: add "reference:" to the front matter, it names the file and fills the cover`)
	}
	return filepath.Join(filepath.Dir(mdPath), name+".docx"), nil
}

// safeFileName replaces characters a file name cannot hold with dashes.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s)
	return strings.Trim(strings.TrimSpace(s), ". ")
}
