// Package archive は config・レイヤー・manifest.json を
// docker-archive 形式の単一 tar にまとめる。
// 出力した tar は `docker load` / `podman load` で読み込める。
//
// tar 内のレイアウト:
//
//	<config-hex>.json        … OCI Image Config
//	<layer-hex>/layer.tar    … 非圧縮レイヤー(複数可)
//	manifest.json            … 上2つを参照するインデックス
package archive

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sakurai-ryo/toy-build-oci/internal/image"
	"github.com/sakurai-ryo/toy-build-oci/internal/layer"
)

var epoch = time.Unix(0, 0).UTC()

// WriteDockerArchive は docker-archive 形式の tar を w に書き出す。
//
//   - cfgData    : image.Config.Marshal() が返した JSON bytes
//   - cfgHex     : 同上の digest(16進)。config ファイル名に使う
//   - layers     : 下位→上位の順に並べたレイヤー
//   - repoTags   : 例 []string{"toyimg:latest"}
func WriteDockerArchive(w io.Writer, cfgData []byte, cfgHex string, layers []*layer.Layer, repoTags []string) error {
	tw := tar.NewWriter(w)

	configName := cfgHex + ".json"

	// レイヤーを書き出しつつ、manifest 用のパス一覧を作る。
	layerPaths := make([]string, len(layers))
	for i, l := range layers {
		// 非圧縮なので layer.tar の digest == diff_id。ディレクトリ名に流用する。
		hexID := strings.TrimPrefix(l.DiffID, "sha256:")
		path := hexID + "/layer.tar"
		layerPaths[i] = path
		if err := addFile(tw, path, l.TarBytes); err != nil {
			return fmt.Errorf("write layer %d: %w", i, err)
		}
	}

	if err := addFile(tw, configName, cfgData); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	manifest := image.DockerManifest{{
		Config:   configName,
		RepoTags: repoTags,
		Layers:   layerPaths,
	}}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := addFile(tw, "manifest.json", manifestData); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return tw.Close()
}

// addFile は1つの通常ファイルを tar に追加する。mtime は固定。
func addFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: epoch,
		Format:  tar.FormatGNU,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
