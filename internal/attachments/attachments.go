// Package attachments packs the supporting documents of a report, the spreadsheets
// and captures a reader may want alongside the .docx, into one ZIP.
package attachments

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Dir is the folder a report keeps its attachments in, next to the Markdown file.
const Dir = "attachments"

// Zip packs every file under dir into a ZIP at outPath, keeping the tree below
// dir. It reports whether it wrote anything: a missing or empty dir produces no
// file at all, so a report without attachments stays a single .docx.
//
// The archive streams into a temporary file beside outPath and is renamed into
// place once it is whole, so a failure part way through leaves no half-written
// ZIP behind and nothing incomplete ever carries the report's name. The attachments
// of an incident run to memory dumps and packet captures, which is why the
// archive is not built in memory: the cost of packing them would otherwise be
// the size of everything packed.
func Zip(dir, outPath string) (written bool, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(outPath), ".md2report-attachments-*")
	if err != nil {
		return false, err
	}
	defer func() {
		tmp.Close()
		if !written {
			os.Remove(tmp.Name())
		}
	}()

	zw := zip.NewWriter(tmp)
	files := 0

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		// Only regular files are packed. Opening a symlink reads whatever it
		// points at, and a link left in the folder while working would carry the
		// content of its target out to the client inside the archive.
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		// ZIP names are slash-separated whatever the host, and the attachments keep
		// their own modification times.
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
		files++
		return nil
	})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil // no attachments folder: nothing to pack
	case err != nil:
		return false, err
	case files == 0:
		return false, nil
	}
	if err := zw.Close(); err != nil {
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	// CreateTemp opens at 0600 and the rename carries the mode over: the attachments
	// of an incident are no more public than the report they belong to.
	if err := os.Rename(tmp.Name(), outPath); err != nil {
		return false, err
	}
	return true, nil
}
