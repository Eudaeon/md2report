// md2report fills a Word incident-report template from a Markdown file. It
// regenerates the cover page, the table of contents and the body, and copies the
// rest of the template through untouched: styles, headers, footers, logos.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"md2report/internal/attachments"
	"md2report/internal/docx"
)

// version is stamped at build time by the release workflow, with the tag it is
// building. It is left empty everywhere else so that a binary not built from a
// release still says something true about where it came from.
var version = ""

// buildVersion is what -version prints. Failing a stamp, the toolchain still
// knows: a `go install md2report@v1.2.3` records the version it fetched, and a
// build from a checkout records "(devel)". A report is evidence, so the first
// question about a document that came out wrong is which build wrote it.
func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "unknown"
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main, minus the exiting, so that a test can call it.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("md2report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tplPath := fs.String("template", "Template.docx", ".docx template to fill")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [options] report.md\n\n"+
			"The document is written next to the Markdown file, named after the\n"+
			"incident reference declared in the front matter. Anything in the\n"+
			"attachments/ folder beside it is packed into a ZIP of the same name.\n\nOptions:\n", filepath.Base(os.Args[0]))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "md2report", buildVersion())
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	out, warnings, err := docx.Generate(fs.Arg(0), *tplPath)
	for _, w := range warnings {
		fmt.Fprintln(stderr, "warning:", w)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, "written:", out)

	// The attachments travel beside the report, under its name. Which name that
	// is, and what it ends in, are docx.Generate's business: the extension comes
	// off the path it returned rather than being spelled out a second time here.
	mdDir := filepath.Dir(fs.Arg(0))
	zipPath := strings.TrimSuffix(out, filepath.Ext(out)) + "-attachments.zip"
	written, err := attachments.Zip(filepath.Join(mdDir, attachments.Dir), zipPath)
	if err != nil {
		fmt.Fprintln(stderr, "error: packing the attachments:", err)
		return 1
	}
	if written {
		fmt.Fprintln(stdout, "written:", zipPath)
	}
	return 0
}
