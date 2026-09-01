<div align="center">

# md2report

*A CLI tool that turns a Markdown incident report into a Word document*

</div>

md2report fills a Word template. It writes the cover page from the front matter, rebuilds the table of contents from the level-1 headings, and renders the body in the styles the template defines. Styles, headers, footers, logos and page layout come through unchanged.

## Install

Prebuilt binaries for Linux, macOS and Windows, on `amd64` and `arm64`, are on the [releases page](https://github.com/Eudaeon/md2report/releases). Each archive contains the binary, this README, the [changelog](CHANGELOG.md) and a copy of `Template.docx`.

> [!NOTE]
> `-template` defaults to `Template.docx` in the working directory, not next to the binary. Keep a copy of the template wherever you run md2report.

Building from source needs Go 1.27 and no dependencies.

```bash
go build -o md2report ./cmd/md2report
```

## Usage

```bash
./md2report report.md
```

The document is written next to the Markdown file, named after the incident reference, so `example/report.md` produces `example/IR-COMPANY-002.docx`. Characters that cannot appear in a file name become dashes. Front matter without a `reference` stops the run.

`-template` selects another template, `-version` prints the version. The full example is in [`example/report.md`](example/report.md).

## Front matter

Front matter declares the cover fields. Any key left out keeps the template's own value.

```markdown
---
title: Incident report
date: Thursday 25 July 2025
type: Phishing
author: C. PETIT
reference: IR-COMPANY-002
---
```

`author` fills the document's author in *File ▸ Properties* and is not printed on the page. `reference` names the produced file, fills the reference on the cover page, and replaces the reference the template prints in its footers. It is copied as written, so any numbering scheme works.

## Variables

Any other front-matter key defines a variable. Write it as `{{name}}` anywhere in the document: text, headings, captions, table cells, links, image paths and code blocks. Keys are matched without regard to case, and a value in quotes loses them. The five reserved keys are variables too.

```markdown
---
reference: IR-COMPANY-002
victim: A. MARTIN
email: alice.martin@example.com
---

# Summary

An email was sent from the account of {{victim}}
([{{email}}](mailto:{{email}})).
```

A variable may use another at any depth, in any declaration order. An unknown variable, or one caught in a cycle, stays in the document as written and md2report warns on standard error. A value is data, not Markdown, so one beginning with `#` or `-` reads as those characters and `**bold**` keeps its asterisks.

## Body

| Markdown                                            | Word style                                                |
| --------------------------------------------------- | --------------------------------------------------------- |
| `# Heading`                                         | `Part` (and an entry in the table of contents)            |
| `## Subheading`, `### …`                            | `Heading2`, `Heading3`, …                                 |
| paragraph                                           | `Body`                                                    |
| `- item`, `* item`, `+ item`                        | `Bullet` (the template's bullets)                         |
| `1. item`, `1) item`                                | `Bullet` with decimal numbering, restarted at each list   |
| `![Caption](image.png)`                             | `Image` + a `Figure N: …` caption, numbered automatically |
| `![Caption](image.png){width=60%}`                  | the same, at the width asked for                          |
| `\| a \| b \|`                                      | a bordered table, first row as header                     |
| `> text`                                            | `Quote`                                                   |
| ```` ``` ````                                       | monospaced block                                          |
| `**bold**`, `*italic*`, `` `code` ``, `[link](url)` | run formatting                                            |

Image paths are relative to the Markdown file, in PNG, JPEG or GIF. `{width=60%}` is a percentage of the usable page width, `{width=8cm}` and `{width=480px}` are absolute, in `px`, `pt`, `cm`, `mm` or `in`, and a bare `{width=480}` is read as pixels. The height scales in proportion, and an image too large for the page is shrunk to fit.

`~~~` also opens a code fence, and `__bold__` and `_italic_` also work. A line that is not empty and carries no new marker joins the paragraph or list item above it, so a wrapped line reading `#1 by severity` stays part of its paragraph. Nesting counts two spaces per level, and a tab counts as two spaces.

> [!WARNING]
> Only `http`, `https` and `mailto` destinations become clickable. Word follows a link wherever it points, so a `\\host\share` or a `file://` would hand the reader's machine, and its credentials, to whoever wrote the address. Any other destination keeps its text but is not made a link.

## Attachments

An `attachments/` folder next to the Markdown file is packed into one ZIP named after the document, so `example/report.md` also produces `example/IR-COMPANY-002-attachments.zip`, sub-folders and all. md2report copies these files without reading or referencing them. An empty or absent folder produces no archive.

## Templates

md2report fills the regions a template declares. To declare one, wrap it in a Word content control (*Developer ▸ Rich Text Content Control*) whose **tag** names it:

| Tag                   | What goes there                                       |
| --------------------- | ----------------------------------------------------- |
| `md2report:title`     | the cover title                                       |
| `md2report:date`      | the cover date                                        |
| `md2report:type`      | the cover incident type                               |
| `md2report:reference` | the cover reference                                   |
| `md2report:toc`       | the table of contents                                 |
| `md2report:body`      | the report, replacing whatever the template put there |

`md2report:reference` and `md2report:body` are required. The other three are optional, but front matter setting a field the template does not declare stops the run. The text printed on the page is never read, so the labels above the cover fields can be reworded or translated.

The body is written in a fixed set of styles:

| Role                    | What it styles                  |
| ----------------------- | ------------------------------- |
| `Part`                  | a level-1 heading               |
| `Heading2` … `Heading6` | the headings below it           |
| `Body`                  | a paragraph                     |
| `Bullet`                | a list item                     |
| `Quote`                 | a block quotation               |
| `Image`                 | a figure                        |
| `FigureCaption`         | the caption under it            |
| `Contents`              | a line of the table of contents |
| `Link`                  | a link in the body              |
| `Hyperlink`             | a link in the table of contents |
| `NormalTable`           | a table                         |

`Heading2` to `Heading6`, `Quote`, `Hyperlink` and `NormalTable` are Word's own, and md2report finds them under a localized name too, so a template authored in a French Word storing `Heading2` as `Titre2` works. The seven others are the template's, and a template using its own name for one declares that name in a custom document property (*File ▸ Info ▸ Properties ▸ Advanced Properties ▸ Custom*), so a template whose body style is `Contenu` declares `md2report:style:Body` with the value `Contenu`. A template that leaves a needed style unresolved is refused, and the error names the property that would resolve it.

`md2report:caption` sets the text introducing a figure caption, where `{n}` stands for the figure number. It defaults to `Figure {n}: `.

> [!TIP]
> Word recomputes the page numbers in the table of contents when the document opens. Under LibreOffice, run *Tools ▸ Update ▸ Update All*.
