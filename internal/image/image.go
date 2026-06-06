// Package image は OCI Image Config と、docker-archive 形式の
// manifest.json を組み立てる。ここで「設定」と「レイヤー」が
// digest で結びつけられる様子が、イメージの本質。
package image

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Config は OCI Image Config の最小サブセット。
// 実際のフィールドはもっと多いが、動かすのに必要な分だけ定義する。
type Config struct {
	Architecture string    `json:"architecture"`
	OS           string    `json:"os"`
	Config       RunConfig `json:"config"`
	RootFS       RootFS    `json:"rootfs"`
	History      []History `json:"history,omitempty"`
}

// RunConfig はコンテナ実行時の振る舞い(Cmd/Env等)。
// JSONキーが大文字始まりなのは Docker/OCI の慣習に合わせるため。
type RunConfig struct {
	Env        []string `json:"Env,omitempty"`
	Cmd        []string `json:"Cmd,omitempty"`
	Entrypoint []string `json:"Entrypoint,omitempty"`
	WorkingDir string   `json:"WorkingDir,omitempty"`
}

// RootFS はレイヤーの積層を diff_id の並びで表す。
type RootFS struct {
	Type    string   `json:"type"`     // 常に "layers"
	DiffIDs []string `json:"diff_ids"` // 下位→上位の順(非圧縮tarのsha256)
}

// History は各レイヤーの由来を記録する(任意項目)。
type History struct {
	CreatedBy  string `json:"created_by,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// Marshal は Config を JSON 化し、その内容の digest(config digest)を返す。
// 返した bytes はそのまま tar に書き込む必要がある(digest は内容のハッシュなので、
// 再シリアライズすると値がずれる可能性があるため)。
func (c *Config) Marshal() (data []byte, digestHex string, err error) {
	data, err = json.Marshal(c)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// DockerManifest は docker-archive(`docker save`クラシック形式)の
// manifest.json。配列の各要素が1イメージを表す。
type DockerManifest []DockerManifestEntry

type DockerManifestEntry struct {
	Config   string   `json:"Config"`   // tar内のconfigファイルへのパス
	RepoTags []string `json:"RepoTags"` // 例: ["toyimg:latest"]
	Layers   []string `json:"Layers"`   // tar内の各layer.tarへのパス(下位→上位)
}
