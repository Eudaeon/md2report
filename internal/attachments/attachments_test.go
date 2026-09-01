package attachments

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// read opens a ZIP and returns its entries, name to content.
func read(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(data)
	}
	return out
}

func TestZipPacksTheTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"recipients.xlsx":      "fake spreadsheet",
		"logs/connections.csv": "ip,date\n93.184.216.34,25/07\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "IR-002-attachments.zip")
	written, err := Zip(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("attachments were present, the archive should have been written")
	}

	entries := read(t, out)
	if len(entries) != 2 {
		t.Fatalf("%d entries in the archive, 2 expected: %v", len(entries), entries)
	}
	if entries["recipients.xlsx"] != "fake spreadsheet" {
		t.Errorf("recipients.xlsx = %q", entries["recipients.xlsx"])
	}
	// A sub-folder keeps its path, with forward slashes whatever the host.
	if _, ok := entries["logs/connections.csv"]; !ok {
		t.Errorf("the sub-folder was flattened or dropped: %v", entries)
	}
}

// No attachments folder, or an empty one, leaves the report a single .docx.
func TestZipWritesNothingWithoutAttachments(t *testing.T) {
	out := filepath.Join(t.TempDir(), "IR-002-attachments.zip")

	for _, dir := range []string{filepath.Join(t.TempDir(), "absent"), t.TempDir()} {
		written, err := Zip(dir, out)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if written {
			t.Errorf("%s: nothing to pack, yet the archive was reported written", dir)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s: an archive was created for nothing", dir)
		}
	}
}

// A link left in the folder while working would carry the content of whatever it
// points at out to the client, inside the archive the report ships with.
func TestZipSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "creds.txt")
	if err := os.WriteFile(secret, []byte("CONFIDENTIAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachments := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(attachments, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "capture.txt"), []byte("a capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(attachments, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	out := filepath.Join(dir, "report-attachments.zip")
	written, err := Zip(attachments, out)
	if err != nil || !written {
		t.Fatalf("Zip() = (%v, %v)", written, err)
	}
	got := read(t, out)
	if _, ok := got["link.txt"]; ok {
		t.Errorf("the symlink was packed: %v", got)
	}
	if got["capture.txt"] != "a capture" {
		t.Errorf("entries = %v, expected the regular file to be packed", got)
	}
}

// The attachments of an incident are no more public than the report they belong to.
func TestZipKeepsTheArchivePrivate(t *testing.T) {
	dir := t.TempDir()
	attachments := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(attachments, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report-attachments.zip")
	if _, err := Zip(attachments, out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, expected 600", got)
	}
}

// The archive is assembled beside its destination and renamed into place, so a
// run that packs nothing must not leave the workings behind.
func TestZipLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	written, err := Zip(empty, filepath.Join(dir, "report-attachments.zip"))
	if err != nil || written {
		t.Fatalf("Zip() = (%v, %v), expected (false, nil)", written, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "attachments" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}
