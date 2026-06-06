# toy-build-oci で学ぶ「OCIイメージの作られ方」

このページは、最小のイメージビルダー [`toy-build-oci`](https://github.com/sakurai-ryo/toy-build-oci) を題材に、
**Docker や BuildKit がイメージを組み立てる過程**を分解して理解するための勉強ノートです。

ゴールは「rootfs ディレクトリ → `docker load` / `podman load` できる tar」を手で組み立てること。
`FROM scratch` 相当の最小イメージを自作し、実際に `docker run` で動かします。

```sh
make run
# => 自作ビルダーでイメージを作り、docker load -> docker run まで実行し
#    "hello from toy-build-oci" を出力する
```

---

## 目次

1. [そもそも「イメージ」とは何の集合か](#1-そもそもイメージとは何の集合か)
2. [ステップ1: レイヤー（ディレクトリ → tar → diff_id）](#2-ステップ1-レイヤーディレクトリ--tar--diff_id)
3. [ステップ2: Image Config（実行設定とレイヤーの台帳）](#3-ステップ2-image-config実行設定とレイヤーの台帳)
4. [ステップ3: Manifest（config と layers を束ねる）](#4-ステップ3-manifestconfig-と-layers-を束ねる)
5. [ステップ4: tar にまとめる（docker-archive レイアウト）](#5-ステップ4-tar-にまとめるdocker-archive-レイアウト)
6. [docker load → run で何が起きているか](#6-docker-load--run-で何が起きているか)
7. [digest の連鎖（content-addressable storage）](#7-digest-の連鎖content-addressable-storage)
8. [もう一つの出力形式: OCI Image Layout（`--format oci`）](#8-もう一つの出力形式-oci-image-layoutformat-oci)
9. [本物の Docker / OCI との違い](#9-本物の-docker--oci-との違い)
10. [用語集](#10-用語集)
11. [次のステップ](#11-次のステップ)

---

## 1. そもそも「イメージ」とは何の集合か

コンテナイメージは1つのファイルではなく、**いくつかの blob（バイト列）とそれらを参照するメタデータ JSON** の集合です。
最小構成では次の3種類だけで成り立ちます。

| 構成要素 | 実体 | 役割 |
|---------|------|------|
| **レイヤー** | 非圧縮 tar | ファイルシステムの中身（rootfs の差分） |
| **Image Config** | JSON | 実行時設定（Cmd/Env 等）＋ どのレイヤーを積むかの台帳 |
| **Manifest** | JSON | Config とレイヤー群を digest で指すインデックス |

関係を1枚にすると、こうなります。

```
rootfs/ ──tar──► layer.tar ──sha256──► diff_id ─┐
                                                ├─► Image Config (JSON) ──sha256──► config digest
Cmd / Env / Arch ───────────────────────────────┘
                                                          │
                                                          ▼
                                       manifest.json が config と layers を参照
```

ポイントは、**上位の要素が下位の要素を「内容のハッシュ（digest）」で指している**こと。
これがコンテナの再配布・キャッシュ・改ざん検知を支える仕組み（content addressing）です。

---

## 2. ステップ1: レイヤー（ディレクトリ → tar → diff_id）

> 実装: [`internal/layer/layer.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/layer/layer.go)

ファイルシステムの中身は **tar** にまとめます。`toy-build-oci` は rootfs ディレクトリを1つの tar に固め、
その **sha256** を取ります。この値を **`diff_id`** と呼びます。

```
diff_id = sha256( 非圧縮のレイヤー tar )
例: sha256:3de7f7977c24d023adb3ed26397443f44bf30fa78391f5b09a3943e80f02515e
```

### なぜ「非圧縮」の sha256 なのか

レイヤーは配布時には gzip で圧縮されることが多いですが、**`diff_id` はあえて非圧縮 tar のハッシュ**です。
圧縮はライブラリやレベルで出力バイトが変わり得るため、圧縮後のハッシュだと「中身は同じなのに別物」と判定されてしまいます。
非圧縮の中身でハッシュを取ることで、**圧縮形式に依存しないレイヤーの同一性**を表せます。

> 「配布用に gzip した tar のハッシュ」は別物で、こちらは Manifest 側で使われます（[後述](#8-本物の-docker--oci-との違い)）。

### 再現可能ビルド（reproducible build）

tar には各ファイルの mtime・uid・gid が含まれます。これらが実行のたびに変わると、
中身が同じでも `diff_id` がブレてしまいます。そこで `toy-build-oci` はヘッダを正規化します。

- エントリをパス名でソート
- `uid/gid = 0`、`uname/gname` を空に
- `mtime` を **epoch（1970-01-01）に固定**

これは BuildKit の `SOURCE_DATE_EPOCH` と同じ発想です。おかげで **同じ入力からは常に同じ digest** が得られます。

```sh
# 2回ビルドしても tar 全体のハッシュが完全一致する
$ shasum -a 256 a.tar out.tar
2e4ea6e36129...  a.tar
2e4ea6e36129...  out.tar
```

---

## 3. ステップ2: Image Config（実行設定とレイヤーの台帳）

> 実装: [`internal/image/image.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/image/image.go)

Image Config は2つの役割を持つ JSON です。

1. **コンテナの実行設定**（`Cmd` / `Env` / `Entrypoint` / `WorkingDir` / `architecture` / `os`）
2. **どのレイヤーをどの順で積むかの台帳**（`rootfs.diff_ids`）

`toy-build-oci` が生成する Config（抜粋）:

```json
{
  "architecture": "arm64",
  "os": "linux",
  "config": {
    "Cmd": ["/hello"]
  },
  "rootfs": {
    "type": "layers",
    "diff_ids": [
      "sha256:3de7f7977c24d023adb3ed26397443f44bf30fa78391f5b09a3943e80f02515e"
    ]
  },
  "history": [
    { "created_by": "toy-build-oci build --from-dir ./testdata/rootfs" }
  ]
}
```

- `rootfs.diff_ids` は **下位 → 上位の順**に並んだレイヤーの diff_id。コンテナ起動時はこの順に重ねて1つの rootfs を作ります。
- `architecture` / `os` が実行環境と合っていないと docker は実行を拒否します（Apple Silicon の Docker Desktop は `linux/arm64`）。

### config digest = JSON そのもののハッシュ

この Config JSON 全体の sha256 を **config digest** と呼びます。

```
config digest = sha256( Config JSON のバイト列 )
例: sha256:c7d19ee70d0e26a4be777eb40bf49e435297f1ea8bc561743ad010f64917ff55
```

> ⚠️ ここが落とし穴: digest は「バイト列のハッシュ」なので、**一度シリアライズした bytes をそのまま保存・参照**しなければなりません。
> 同じ構造体でも再 marshal するとキー順や空白で1バイトでも変われば digest がズレます。
> `toy-build-oci` では `Config.Marshal()` が「JSON bytes」と「その digest」を**同時に**返し、その bytes をそのまま tar に書き込みます。

---

## 4. ステップ3: Manifest（config と layers を束ねる）

> 実装: [`internal/image/image.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/image/image.go) の `DockerManifest`

Manifest は「この config と、これらの layer で1つのイメージ」と宣言するインデックスです。
`toy-build-oci` は **docker-archive 形式**（`docker save` のクラシック形式）の `manifest.json` を出力します。

```json
[
  {
    "Config": "c7d19ee70d0e...json",
    "RepoTags": ["toyimg:latest"],
    "Layers": ["3de7f7977c24.../layer.tar"]
  }
]
```

- `Config` … tar 内の Config ファイルへのパス
- `Layers` … tar 内の各 `layer.tar` へのパス（下位 → 上位）
- `RepoTags` … `docker load` 後に付くタグ

配列になっているのは、1つの tar に複数イメージを詰められるからです。

---

## 5. ステップ4: tar にまとめる（docker-archive レイアウト）

> 実装: [`internal/archive/archive.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/archive/archive.go)

最後に、レイヤー・config・manifest を1つの tar に詰めます。中身はこうなります。

```
$ tar tf out.tar
3de7f7977c24d023adb3ed26397443f44bf30fa78391f5b09a3943e80f02515e/layer.tar
c7d19ee70d0e26a4be777eb40bf49e435297f1ea8bc561743ad010f64917ff55.json
manifest.json
```

- レイヤーは非圧縮なので、`layer.tar` の digest はそのまま `diff_id` と一致します。だからディレクトリ名に流用できます。
- これで `docker load` / `podman load` が読める tar の完成です。

---

## 6. docker load → run で何が起きているか

```sh
$ docker load -i out.tar
Loaded image: toyimg:latest

$ docker run --rm toyimg:latest
hello from toy-build-oci
```

`docker load` 時、Docker は次をやっています。

1. `manifest.json` を読み、`Config` と `Layers` のパスを把握
2. 各 `layer.tar` を展開してローカルのレイヤーストアに保存
3. Config を読み、`rootfs.diff_ids` の順にレイヤーを重ねて rootfs を再構成
4. `RepoTags` のタグを付与

`docker run` 時は、その rootfs をコンテナのファイルシステムとしてマウントし、`config.Cmd`（ここでは `/hello`）をプロセスとして起動します。

```sh
# Docker から見たイメージの素性
$ docker image inspect toyimg:latest \
    --format 'Arch={{.Architecture}} OS={{.Os}} Cmd={{.Config.Cmd}} Layers={{len .RootFS.Layers}}'
Arch=arm64 OS=linux Cmd=[/hello] Layers=1
```

---

## 7. digest の連鎖（content-addressable storage）

ここまでの digest の参照関係をまとめると、**下から上へハッシュが連鎖**しているのが分かります。

```
 ファイル群
    │  tar 化
    ▼
 layer.tar ──sha256─►  diff_id ───────────────┐
                                              │ 台帳として列挙
                                              ▼
                                       Image Config (JSON)
                                              │
                                              │ sha256
                                              ▼
                                        config digest ──┐
                                                        │ 参照
 layer.tar のパス ──────────────────────────────────────┤
                                                        ▼
                                                  manifest.json
```

この「内容のハッシュで指す」構造のおかげで、

- **キャッシュ**: 同じ digest のレイヤーは再ダウンロード・再ビルド不要
- **重複排除**: 複数イメージが同一レイヤーを共有できる
- **改ざん検知**: 1バイトでも変われば digest が変わるので検出できる

が成立します。Git のオブジェクトモデルとよく似た発想です。

---

## 8. もう一つの出力形式: OCI Image Layout（`--format oci`）

ここまでは `docker save` 互換の **docker-archive** 形式でした。`toy-build-oci` は
`--format oci` を付けると、OCI 標準の **Image Layout** を tar（oci-archive）として出力します。
両者を見比べると「Docker独自形式」と「OCI標準形式」の違いがクリアになります。

```sh
toy-build-oci build --from-dir ./testdata/rootfs --tag toyimg:latest \
    --cmd /hello --format oci -o out-oci.tar
```

> 実装: [`internal/archive/archive.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/archive/archive.go) の `WriteOCIArchive`、
> および [`internal/image/image.go`](https://github.com/sakurai-ryo/toy-build-oci/blob/main/internal/image/image.go) の OCI 型定義

### レイアウト: すべてが `blobs/sha256/<digest>` に並ぶ

```
$ tar tf out-oci.tar
blobs/sha256/f692ce0c...   # Image Config
blobs/sha256/f632ae1c...   # レイヤー（gzip 圧縮）
blobs/sha256/99c074b3...   # Image Manifest
index.json                 # トップレベルのインデックス
oci-layout                 # {"imageLayoutVersion":"1.0.0"}
```

docker-archive ではファイル名が `<hex>/layer.tar` や `manifest.json` という「役割ベースのパス」でしたが、
OCI では **すべての blob が中身の digest そのものをファイル名**にして `blobs/sha256/` に並びます。
これは前章の content addressing をディレクトリ構造として体現したものです。

### `index.json` → manifest → config/layers と descriptor で辿る

入口は `index.json`。ここから **descriptor**（`mediaType` + `digest` + `size` を持つ参照）で下へ辿ります。

```json
// index.json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [{
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "digest": "sha256:99c074b3...",
    "size": 405,
    "annotations": { "org.opencontainers.image.ref.name": "toyimg:latest" },
    "platform": { "architecture": "arm64", "os": "linux" }
  }]
}
```

```json
// blobs/sha256/99c074b3...  (Image Manifest)
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:f692ce0c...",
    "size": 257
  },
  "layers": [{
    "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
    "digest": "sha256:f632ae1c...",
    "size": 1332284
  }]
}
```

docker-archive の `manifest.json` との違いは明確です。

- **`mediaType`** … 各 blob が「何者か」を明示（config / manifest / gzip レイヤー）。tar の中身を開かずに型が分かる
- **`size`** … blob のバイト数。レジストリからのダウンロード前に必要量が分かる
- **`platform` / `annotations`** … index がどの arch/os 向けか、どのタグかを宣言（マルチアーキ対応の土台）

### 1つのレイヤーに2つの digest

OCI 形式で初めて、**1レイヤーに digest が2つ**登場します。

| digest | 何のハッシュか | どこに入るか |
|--------|----------------|--------------|
| `diff_id` | **非圧縮** tar の sha256 | Image Config の `rootfs.diff_ids` |
| レイヤー digest | **gzip 圧縮後** の sha256 | Image Manifest の `layers[].digest` |

```
            ┌──────────────► diff_id          → Image Config (rootfs.diff_ids)
 layer.tar ─┤  sha256(非圧縮)
            │
            └─ gzip ─► layer.tar.gz ─ sha256 ─► layer digest → Image Manifest (layers[].digest)
```

- **Config 側が非圧縮 (`diff_id`)** なのは、圧縮方式を変えても rootfs の同一性が保たれるようにするため
- **Manifest 側が圧縮後** なのは、実際にネットワークを流れる・保存される blob がその圧縮済みバイト列だから

`toy-build-oci` の `Layer.Gzip()` がこの圧縮後 digest を計算しています。Go の gzip は
ヘッダのタイムスタンプを 0 にするため、**圧縮後 digest も再現可能**です（docker-archive と同じく2回ビルドで完全一致）。

### 読み込み

```sh
podman load -i out-oci.tar
# あるいは
skopeo copy oci-archive:out-oci.tar docker-daemon:toyimg:latest
# Docker 24+（containerd image store 有効時）なら docker load も可
docker load -i out-oci.tar
```

---

## 9. 本物の Docker / OCI との違い

OCI Image Layout（前章）まで実装したことで、形式面での差はかなり埋まりました。
**残る主な違いは「配布」**です。

### レジストリへの push（残るギャップ）

実運用では tar をやり取りせず、**OCI Distribution API**（HTTP）でレジストリに push/pull します。
blob を `PUT /v2/<name>/blobs/...`、manifest を `PUT /v2/<name>/manifests/<tag>` で送る流れです（→ ロードマップ M5）。

### その他の簡略化

- **単一レイヤーのみ**: 本ツールは rootfs を1レイヤーにまとめます。実際の Dockerfile は命令ごとにレイヤーを積みます（→ M3）。
- **圧縮は gzip 固定**: 実際には zstd（`...tar+zstd`）も使われます。
- **whiteout 未対応**: 上位レイヤーでファイルを「削除」する `.wh.` マーカーは扱いません。
- **annotations/created 等のメタデータ**: 最小限のみ設定しています。

| 観点 | toy-build-oci | 本物の Docker/OCI |
|------|---------------|-------------------|
| 出力形式 | docker-archive **/ OCI Image Layout** | 同左 ＋ レジストリ |
| レイヤー圧縮 | gzip（`oci`）/ 非圧縮（`docker`） | gzip / zstd |
| レイヤー数 | 常に1 | Dockerfile の命令ごと |
| 配布 | `docker load` / `podman load` | registry へ push/pull |

---

## 10. 用語集

| 用語 | 意味 |
|------|------|
| **レイヤー (layer)** | ファイルシステムの差分を表す tar |
| **diff_id** | 非圧縮レイヤー tar の sha256。Config の `rootfs.diff_ids` に入る |
| **Image Config** | 実行設定とレイヤー台帳を持つ JSON |
| **config digest** | Config JSON 全体の sha256 |
| **Manifest** | config とレイヤーを digest で束ねるインデックス |
| **digest** | コンテンツの sha256（`sha256:<hex>`）。content addressing の鍵 |
| **descriptor** | `mediaType`+`digest`+`size` を持つ blob への参照。OCI 形式での指し方 |
| **mediaType** | blob の種別を表す文字列（例 `...image.layer.v1.tar+gzip`） |
| **index.json** | OCI Image Layout の入口。manifest を descriptor で指す |
| **oci-layout** | OCI レイアウトであることを示すマーカーファイル |
| **docker-archive** | `docker save` 互換の tar 形式。`--format docker`（既定） |
| **OCI Image Layout** | OCI 標準の構造（`blobs/`・`index.json`・`oci-layout`）。`--format oci` |
| **再現可能ビルド** | mtime 等を固定し、同じ入力から同じ digest を得る手法 |

---

## 11. 次のステップ

- [x] **M1** 単一レイヤー → docker-archive tar → `docker load` / `docker run`
- [x] **M2** Cmd/Env/Entrypoint/WorkingDir を Config に反映
- [ ] **M3** 複数レイヤー対応（レイヤーの積層と `diff_ids` の順序を体感）
- [x] **M4** gzip 圧縮 + 正式な OCI Image Layout 出力（`--format oci`、[§8](#8-もう一つの出力形式-oci-image-layoutformat-oci) で解説）
- [ ] **M5** レジストリ push（OCI Distribution API、[§9](#9-本物の-docker--oci-との違い) の残るギャップ）

ソース全体は [GitHub リポジトリ](https://github.com/sakurai-ryo/toy-build-oci) を参照してください。
