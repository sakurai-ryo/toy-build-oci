# toy-build-oci

OCIイメージの作られ方を理解するための、最小のイメージビルダー CLI（Go製）。
rootfs ディレクトリから **`docker load` / `podman load` 可能な tar**（docker-archive 形式）を生成する。

## クイックスタート

```sh
make run
# => 自作ビルダーでイメージを作り、docker load → docker run まで実行し
#    "hello from toy-build-oci" を出力する
```

手動で実行する場合:

```sh
make build hello                                  # CLIと検証用helloを用意
./bin/toy-build-oci build \
    --from-dir ./testdata/rootfs \
    --tag toyimg:latest \
    --cmd /hello -o out.tar
docker load -i out.tar
docker run --rm toyimg:latest
```

## CLI

```
toy-build-oci build [flags]

  --from-dir DIR     レイヤーにするrootfsディレクトリ (必須)
  --tag NAME:TAG     イメージのタグ (default: toyimg:latest)
  --cmd ARG          コンテナのCmd (繰り返し可)
  --env KEY=VAL      環境変数 (繰り返し可)
  --entrypoint ARG   Entrypoint (繰り返し可)
  --workdir DIR      作業ディレクトリ
  --arch ARCH        アーキテクチャ (default: 実行環境のGOARCH)
  --os OS            OS (default: linux)
  -o FILE            出力tarのパス (default: out.tar)
```

## 仕組み（OCIイメージの構成要素）

```
rootfs/ ──tar──► layer.tar ──sha256──► diff_id ─┐
                                                ├─► Image Config(JSON) ─sha256─► config digest
Cmd/Env/Arch ───────────────────────────────────┘
                                                          │
                                                          ▼
                                            manifest.json が config と layers を参照
```

生成される tar のレイアウト（docker-archive 形式）:

```
<config-hex>.json        … OCI Image Config
<layer-hex>/layer.tar    … 非圧縮レイヤー
manifest.json            … 上2つを参照するインデックス
```

- **diff_id** は「非圧縮」レイヤーtarの sha256。圧縮形式に依存しないレイヤー同一性を表す。
- tarヘッダの mtime/uid/gid を正規化しているため、同じ入力からは **常に同じ digest**（再現可能ビルド）になる。

## コード構成

| パス | 役割 |
|------|------|
| `cmd/toy-build-oci` | CLI（フラグ解析） |
| `internal/layer`    | ディレクトリ→非圧縮tar、diff_id算出 |
| `internal/image`    | OCI Image Config / manifest.json 構築 |
| `internal/archive`  | docker-archive tar の書き出し |
| `hello/`            | 検証用の静的リンクバイナリ |

## ロードマップ

- [x] **M1** 単一レイヤー → docker-archive tar → `docker load`/`docker run`
- [x] M2 Cmd/Env/Entrypoint/WorkingDir をConfigに反映
- [ ] M3 複数レイヤー対応（`--add-dir` 複数 or 簡易Dockerfile）
- [ ] M4 gzip圧縮 + 正式な OCI Image Layout（`blobs/`, `index.json`, `oci-layout`）
- [ ] M5 レジストリ push（OCI Distribution API）
```
