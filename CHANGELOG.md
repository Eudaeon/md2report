# Changelog

What changed in each release, for the people who read a report before they read
the code. The release workflow publishes the section named after the tag it is
building, so `v1.2.3` needs a `## v1.2.3` heading here before that tag is
pushed.

## v1.0.0

First release.

### Added

- `md2report report.md` fills a Word template: the cover page from the front
  matter, the table of contents from the level-1 headings, the body in the
  styles the template defines. Everything else in the template comes through
  untouched.
- Front matter declares the cover fields. Any other key becomes a `{{variable}}`
  usable in text, headings, captions, table cells, links, image paths and code
  blocks, at any depth and in any order.
- Headings, paragraphs, bullets, numbered lists, quotations, code blocks,
  tables, and images with a caption and a width. Bold, italic, inline code and
  links inside any of them.
- A template says which style it uses for a role through the custom document
  property `md2report:style:Role`, and marks the regions md2report rewrites with
  content controls. A missing style stops the run rather than producing a report
  stripped of its formatting.
- Anything under `attachments/` beside the Markdown is packed into a ZIP named
  after the document.
- `-version` prints the tag the binary was built from.
- Prebuilt binaries for Linux, macOS and Windows, on amd64 and arm64. Each
  archive carries the binary, the README, this changelog and `Template.docx`.
