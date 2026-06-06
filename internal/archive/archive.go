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
	"crypto/sha256"
	"encoding/hex"
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

// WriteOCIArchive writes an OCI Image Layout as a tar (an "oci-archive") to w.
// It can be read by `podman load`, `skopeo copy oci-archive:...`, and newer
// Docker with the containerd image store.
//
// Layout inside the tar:
//
//	oci-layout                    … marker: {"imageLayoutVersion":"1.0.0"}
//	index.json                    … top-level index pointing at the manifest
//	blobs/sha256/<config-hex>     … OCI Image Config
//	blobs/sha256/<layer-hex>      … gzip-compressed layer(s)
//	blobs/sha256/<manifest-hex>   … OCI Image Manifest
//
// Compared with docker-archive: blobs are content-addressed by digest, every
// reference is a typed descriptor (mediaType + size), and layers are gzipped.
func WriteOCIArchive(w io.Writer, cfgData []byte, cfgHex string, layers []*layer.Layer, ref, arch, osName string) error {
	tw := tar.NewWriter(w)

	// blobs/sha256/<config-hex>: the Image Config (reused as-is from the caller).
	if err := addFile(tw, blobPath(cfgHex), cfgData); err != nil {
		return fmt.Errorf("write config blob: %w", err)
	}

	// blobs/sha256/<layer-hex>: gzip each layer and record its descriptor.
	layerDescs := make([]image.Descriptor, len(layers))
	for i, l := range layers {
		gz, digest, size, err := l.Gzip()
		if err != nil {
			return fmt.Errorf("gzip layer %d: %w", i, err)
		}
		if err := addFile(tw, blobPath(strings.TrimPrefix(digest, "sha256:")), gz); err != nil {
			return fmt.Errorf("write layer blob %d: %w", i, err)
		}
		layerDescs[i] = image.Descriptor{
			MediaType: image.MediaTypeImageLayerGzip,
			Digest:    digest,
			Size:      size,
		}
	}

	// The Image Manifest ties config + layers together via descriptors.
	manifest := image.Manifest{
		SchemaVersion: 2,
		MediaType:     image.MediaTypeImageManifest,
		Config: image.Descriptor{
			MediaType: image.MediaTypeImageConfig,
			Digest:    "sha256:" + cfgHex,
			Size:      int64(len(cfgData)),
		},
		Layers: layerDescs,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifestHex := sha256Hex(manifestData)
	if err := addFile(tw, blobPath(manifestHex), manifestData); err != nil {
		return fmt.Errorf("write manifest blob: %w", err)
	}

	// index.json points at the manifest, tagging it with the ref name + platform.
	index := image.Index{
		SchemaVersion: 2,
		MediaType:     image.MediaTypeImageIndex,
		Manifests: []image.Descriptor{{
			MediaType:   image.MediaTypeImageManifest,
			Digest:      "sha256:" + manifestHex,
			Size:        int64(len(manifestData)),
			Annotations: map[string]string{image.AnnotationRefName: ref},
			Platform:    &image.Platform{Architecture: arch, OS: osName},
		}},
	}
	indexData, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if err := addFile(tw, "index.json", indexData); err != nil {
		return fmt.Errorf("write index.json: %w", err)
	}

	// oci-layout marker.
	layoutData, err := json.Marshal(image.Layout{ImageLayoutVersion: "1.0.0"})
	if err != nil {
		return err
	}
	if err := addFile(tw, "oci-layout", layoutData); err != nil {
		return fmt.Errorf("write oci-layout: %w", err)
	}

	return tw.Close()
}

func blobPath(hexDigest string) string { return "blobs/sha256/" + hexDigest }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
