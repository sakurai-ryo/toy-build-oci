// Package archive bundles the config, layers, and manifest.json into a
// single docker-archive tar. The resulting tar can be read by
// `docker load` / `podman load`.
//
// Layout inside the tar:
//
//	<config-hex>.json        … OCI Image Config
//	<layer-hex>/layer.tar    … uncompressed layer (one or more)
//	manifest.json            … index referencing the two above
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

// WriteDockerArchive writes a docker-archive format tar to w.
//
//   - cfgData  : the JSON bytes returned by image.Config.Marshal()
//   - cfgHex   : its digest (hex); used as the config file name
//   - layers   : layers ordered lowest -> highest
//   - repoTags : e.g. []string{"toyimg:latest"}
func WriteDockerArchive(w io.Writer, cfgData []byte, cfgHex string, layers []*layer.Layer, repoTags []string) error {
	tw := tar.NewWriter(w)

	configName := cfgHex + ".json"

	// Write the layers and collect the path list for the manifest.
	layerPaths := make([]string, len(layers))
	for i, l := range layers {
		// Uncompressed, so the layer.tar digest == diff_id. Reuse it as the directory name.
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

// addFile adds a single regular file to the tar. mtime is pinned.
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
