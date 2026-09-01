package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// report writes a Markdown file in a fresh directory and returns its path.
func report(t *testing.T, frontMatter string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	if err := os.WriteFile(path, []byte(frontMatter+"\n# Findings\n\nNothing to report.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const frontMatter = "---\ntitle: Report\ndate: 12 March 2026\ntype: Phishing\nreference: IR-COMPANY-002\n---\n"

func run1(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	code = run(append([]string{"-template", filepath.Join("..", "..", "Template.docx")}, args...), &out, &errs)
	return code, out.String(), errs.String()
}

func TestWithoutAttachmentsOnlyTheReportIsWritten(t *testing.T) {
	md := report(t, frontMatter)

	code, stdout, stderr := run1(t, md)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}

	docxPath := filepath.Join(filepath.Dir(md), "IR-COMPANY-002.docx")
	if _, err := os.Stat(docxPath); err != nil {
		t.Errorf("the report should be named after the reference: %v", err)
	}
	if !strings.Contains(stdout, docxPath) {
		t.Errorf("the report path should be announced, got %q", stdout)
	}
	if strings.Contains(stdout, "attachments.zip") {
		t.Errorf("no attachments, so no ZIP should be announced, got %q", stdout)
	}
	if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(md), "*.zip")); len(entries) != 0 {
		t.Errorf("no attachments, so no ZIP should be written, got %v", entries)
	}
}

func TestAttachmentsArePackedUnderTheReportName(t *testing.T) {
	md := report(t, frontMatter)
	dir := filepath.Dir(md)
	if err := os.MkdirAll(filepath.Join(dir, "attachments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "attachments", "recipients.xlsx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run1(t, md)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}

	zipPath := filepath.Join(dir, "IR-COMPANY-002-attachments.zip")
	if _, err := os.Stat(zipPath); err != nil {
		t.Errorf("the ZIP should sit beside the report, under its name: %v", err)
	}
	if !strings.Contains(stdout, zipPath) {
		t.Errorf("the ZIP path should be announced, got %q", stdout)
	}
}

func TestAReportWithoutReferenceFails(t *testing.T) {
	md := report(t, "---\ntitle: Report\ndate: 12 March 2026\ntype: Phishing\n---\n")

	code, stdout, stderr := run1(t, md)
	if code != 1 {
		t.Fatalf("exit %d, expected 1", code)
	}
	if !strings.Contains(stderr, "no reference") {
		t.Errorf("the error should say what is missing, got %q", stderr)
	}
	if stdout != "" {
		t.Errorf("nothing should be announced when nothing was written, got %q", stdout)
	}
}

func TestUnresolvedVariablesAreWarnedAboutBeforeTheReportPath(t *testing.T) {
	md := report(t, frontMatter+"\nThe {{unknown}} account was locked.\n")

	code, stdout, stderr := run1(t, md)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "{{unknown}}") {
		t.Errorf("an undefined variable should be warned about, got %q", stderr)
	}
	if !strings.Contains(stdout, ".docx") {
		t.Errorf("the report is still written, got %q", stdout)
	}
}

func TestWrongNumberOfArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"a.md", "b.md"}} {
		var out, errs bytes.Buffer
		if code := run(args, &out, &errs); code != 2 {
			t.Errorf("%v: exit %d, expected 2", args, code)
		}
		if !strings.Contains(errs.String(), "Usage:") {
			t.Errorf("%v: usage should be printed", args)
		}
	}
}

// A report is evidence, so the first question about a document that came out
// wrong is which build wrote it.
func TestVersionFlagReportsTheStampedVersion(t *testing.T) {
	defer func(previous string) { version = previous }(version)
	version = "v1.2.3"

	code, stdout, stderr := run1(t, "-version")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if stdout != "md2report v1.2.3\n" {
		t.Errorf("stdout = %q, expected the stamped version", stdout)
	}
}

// An unstamped build still has to answer, and answer without a file to work on.
func TestVersionFlagAnswersWithoutAStampOrAReport(t *testing.T) {
	defer func(previous string) { version = previous }(version)
	version = ""

	code, stdout, stderr := run1(t, "-version")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "md2report ") || strings.TrimSpace(stdout) == "md2report" {
		t.Errorf("stdout = %q, expected a version after the name", stdout)
	}
}
