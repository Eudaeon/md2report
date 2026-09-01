// Package markdown reads the Markdown a report is written in: the front matter
// that fills the cover page, the body cut into blocks, and the {{variables}}
// expanded into both. It knows nothing of Word.
package markdown

// Inline is a run of text with uniform formatting inside a block.
type Inline struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Href   string // non-empty: the run is a link
}

// Block is a top-level block of the Markdown document. Each kind carries only
// its own fields: a Heading has no caption, an Image has no level, so a
// malformed block cannot be built in the first place.
type Block interface{ block() }

// Heading is a title. Level runs from 1 to 6; level 1 opens a part and gets an
// entry in the table of contents.
type Heading struct {
	Level   int
	Inlines []Inline
}

// Paragraph is a run of body text.
type Paragraph struct{ Inlines []Inline }

// Bullet is a bulleted list item. Level is the nesting depth, from 0.
type Bullet struct {
	Level   int
	Inlines []Inline
}

// Ordered is a numbered list item. Items of the same list share a ListID, so
// numbering restarts at every new list.
type Ordered struct {
	Level   int
	ListID  int
	Inlines []Inline
}

// Quote is a block quotation.
type Quote struct{ Inlines []Inline }

// Code is a monospaced block, kept verbatim.
type Code struct{ Text string }

// Image is an image alone on its line, with its caption and the width asked
// for. Path is relative to the Markdown file.
type Image struct {
	Path    string
	Caption []Inline
	Width   ImageWidth
}

// Table is a table: rows of cells, the first one a header.
type Table struct{ Rows [][][]Inline }

func (Heading) block()   {}
func (Paragraph) block() {}
func (Bullet) block()    {}
func (Ordered) block()   {}
func (Quote) block()     {}
func (Code) block()      {}
func (Image) block()     {}
func (Table) block()     {}

// ImageWidth is the width asked for an image, as the Markdown wrote it: a
// number and the unit it was given in. Unit is empty when the Markdown says
// nothing, and the image then keeps its native size.
type ImageWidth struct {
	Value float64
	Unit  string // "%", "px", "pt", "cm", "mm" or "in"
}

// Document is the result of parsing a Markdown file.
type Document struct {
	Meta    Meta
	Blocks  []Block
	Unknown []string // variables used in the text but never defined
}
