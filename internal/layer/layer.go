// Package layer はディレクトリをOCIイメージの「レイヤー」(非圧縮tar)へ
// 変換する。レイヤーのdigestはコンテンツアドレッシングの根幹なので、
// 再現可能(deterministic)になるよう tar ヘッダを正規化する点が肝。
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

// epoch は ModTime を固定するための基準時刻(1970-01-01)。
// これにより同じ入力からは常に同じ digest が得られる。
// BuildKit の SOURCE_DATE_EPOCH に相当する考え方。
var epoch = time.Unix(0, 0).UTC()

// Layer は単一の非圧縮レイヤーtarと、その diff_id を保持する。
//
// diff_id は「非圧縮」tarの sha256。OCI Image Config の rootfs.diff_ids に
// 入る値で、圧縮形式に依存しないレイヤーの同一性を表す。
type Layer struct {
	TarBytes []byte // 非圧縮レイヤーtarの中身
	DiffID   string // "sha256:<hex>"
}

// FromDir は srcDir 配下のファイル群を1つの非圧縮tarレイヤーにまとめる。
//
// 再現性のため以下を正規化する:
//   - エントリをパス名でソート
//   - uid/gid=0, uname/gname=空
//   - mtime を epoch に固定
func FromDir(srcDir string) (*Layer, error) {
	type entry struct {
		path string // 実ファイルへのパス
		name string // tar内での名前(srcDir基準・スラッシュ区切り)
		info fs.FileInfo
	}

	var entries []entry
	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == srcDir {
			return nil // ルートディレクトリ自体はエントリにしない
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
		// digest を再現可能にするための正規化
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
