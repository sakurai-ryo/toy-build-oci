// Package layer converts a directory into an OCI image "layer" (an
// uncompressed tar). A layer's digest is the foundation of content
// addressing, so the key concern here is making it reproducible
// (deterministic) by normalizing the tar headers.
package layer

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// epoch is the fixed reference time (1970-01-01) used for ModTime.
// With it, the same input always yields the same digest.
// This mirrors BuildKit's SOURCE_DATE_EPOCH idea.
var epoch = time.Unix(0, 0).UTC()

// Layer holds a single uncompressed layer tar and its diff_id.
//
// diff_id is the sha256 of the *uncompressed* tar. It is the value stored
// in the OCI Image Config's rootfs.diff_ids and expresses layer identity
// independently of the compression format.
type Layer struct {
	TarBytes []byte // contents of the uncompressed layer tar
	DiffID   string // "sha256:<hex>"
}

// FromDir packs the files under srcDir into a single uncompressed tar layer.
//
// It normalizes the following for reproducibility:
//   - entries sorted by path name
//   - uid/gid=0, uname/gname empty
//   - mtime pinned to epoch
func FromDir(srcDir string) (*Layer, error) {
	type entry struct {
		path string // path to the actual file
		name string // name inside the tar (relative to srcDir, slash-separated)
		info fs.FileInfo
	}

	var entries []entry
	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == srcDir {
			return nil // do not emit an entry for the root directory itself
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, entry{
			path: p,
			name: filepath.ToSlash(rel),
			info: info,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", srcDir, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		var link string
		if e.info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(e.path); err != nil {
				return nil, fmt.Errorf("readlink %s: %w", e.path, err)
			}
		}
		hdr, err := tar.FileInfoHeader(e.info, link)
		if err != nil {
			return nil, err
		}
		hdr.Name = e.name
		if e.info.IsDir() {
			hdr.Name += "/"
		}
		// Normalize so the digest is reproducible.
		hdr.Uid, hdr.Gid = 0, 0
		hdr.Uname, hdr.Gname = "", ""
		hdr.ModTime = epoch
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
		hdr.Format = tar.FormatGNU

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if e.info.Mode().IsRegular() {
			if err := copyFile(tw, e.path); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(buf.Bytes())
	return &Layer{
		TarBytes: buf.Bytes(),
		DiffID:   "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}
