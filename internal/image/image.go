// Package image assembles the OCI Image Config and the docker-archive
// manifest.json. This is where the "config" and the "layers" get tied
// together by digest, which is the essence of an image.
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Config is a minimal subset of the OCI Image Config.
// The real spec has many more fields; only what is needed to run is defined.
type Config struct {
	Architecture string    `json:"architecture"`
	OS           string    `json:"os"`
	Config       RunConfig `json:"config"`
	RootFS       RootFS    `json:"rootfs"`
	History      []History `json:"history,omitempty"`
}

// RunConfig is the runtime behavior of the container (Cmd/Env/etc).
// The JSON keys are capitalized to match the Docker/OCI convention.
type RunConfig struct {
	Env        []string `json:"Env,omitempty"`
	Cmd        []string `json:"Cmd,omitempty"`
	Entrypoint []string `json:"Entrypoint,omitempty"`
	WorkingDir string   `json:"WorkingDir,omitempty"`
}

// RootFS expresses the stack of layers as an ordered list of diff_ids.
type RootFS struct {
	Type    string   `json:"type"`     // always "layers"
	DiffIDs []string `json:"diff_ids"` // lowest -> highest (sha256 of uncompressed tar)
}

// History records the provenance of each layer (optional).
type History struct {
	CreatedBy  string `json:"created_by,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// Marshal serializes the Config to JSON and returns the digest of its
// contents (the config digest). The returned bytes must be written to the
// tar as-is: the digest is a hash of the contents, so re-serializing could
// change the value.
func (c *Config) Marshal() (data []byte, digestHex string, err error) {
	data, err = json.Marshal(c)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// DockerManifest is the manifest.json of the docker-archive
// (the classic `docker save` format). Each array element is one image.
type DockerManifest []DockerManifestEntry

type DockerManifestEntry struct {
	Config   string   `json:"Config"`   // path to the config file inside the tar
	RepoTags []string `json:"RepoTags"` // e.g. ["toyimg:latest"]
	Layers   []string `json:"Layers"`   // paths to each layer.tar inside the tar (lowest -> highest)
}
