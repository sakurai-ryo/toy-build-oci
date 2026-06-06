# toy-build-oci

A minimal OCI image builder CLI (written in Go), built to understand how container images are actually assembled.
It produces a **`docker load` / `podman load`-able tar** (docker-archive format) from a rootfs directory.

📖 **Study notes (GitHub Pages):** <https://sakurai-ryo.github.io/toy-build-oci/> — a step-by-step walkthrough of how an OCI image is built (in Japanese).

## Quick start

```sh
make run
# => builds an image with the toy builder, runs docker load -> docker run,
#    and prints "hello from toy-build-oci"
```

Run it manually:

```sh
make build hello                                  # build the CLI and the test "hello" binary
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

  --from-dir DIR     rootfs directory to turn into a layer (required)
  --tag NAME:TAG     image tag (default: toyimg:latest)
  --cmd ARG          container Cmd (repeatable)
  --env KEY=VAL      environment variable (repeatable)
  --entrypoint ARG   Entrypoint (repeatable)
  --workdir DIR      working directory
  --arch ARCH        architecture (default: host GOARCH)
  --os OS            OS (default: linux)
  --format FORMAT    "docker" (docker-archive) or "oci" (OCI Image Layout) (default: docker)
  -o, --output FILE  output tar path (default: out.tar)
```

The CLI is built with [Cobra](https://github.com/spf13/cobra); run `toy-build-oci build --help` for the generated help.

### Output formats

```sh
# docker-archive (uncompressed layers, classic `docker save` layout)
toy-build-oci build --from-dir ./testdata/rootfs --cmd /hello -o out.tar

# OCI Image Layout (gzip-compressed layers, blobs/ + index.json + oci-layout)
toy-build-oci build --from-dir ./testdata/rootfs --cmd /hello --format oci -o out-oci.tar
podman load -i out-oci.tar     # or: docker load (Docker 24+), skopeo copy oci-archive:out-oci.tar ...
```

| | `--format docker` | `--format oci` |
|---|---|---|
| Layout | `manifest.json` + `<hex>/layer.tar` | `oci-layout` + `index.json` + `blobs/sha256/<digest>` |
| Layers | uncompressed | gzip-compressed |
| Descriptors | implicit (paths) | typed (mediaType + size + digest) |
| Loaders | `docker load` / `podman load` | `podman load`, `skopeo`, `docker load` (24+) |

## How it works (the pieces of an OCI image)

```
rootfs/ ──tar──► layer.tar ──sha256──► diff_id ─┐
                                                ├─► Image Config (JSON) ─sha256─► config digest
Cmd/Env/Arch ───────────────────────────────────┘
                                                          │
                                                          ▼
                                  manifest.json references the config and layers
```

Layout of the generated tar (docker-archive format):

```
<config-hex>.json        … OCI Image Config
<layer-hex>/layer.tar    … uncompressed layer
manifest.json            … index referencing the two above
```

- **diff_id** is the sha256 of the *uncompressed* layer tar. It expresses layer identity
  independently of the compression format.
- The tar header's mtime/uid/gid are normalized, so the same input always yields the
  **same digest** (reproducible builds).

## Code layout

| Path | Responsibility |
|------|----------------|
| `cmd/toy-build-oci` | CLI (flag parsing) |
| `internal/layer`    | directory → uncompressed tar, diff_id computation |
| `internal/image`    | OCI Image Config / manifest.json assembly |
| `internal/archive`  | docker-archive tar writer |
| `hello/`            | statically linked binary used for verification |

## Roadmap

- [x] **M1** single layer → docker-archive tar → `docker load` / `docker run`
- [x] M2 reflect Cmd/Env/Entrypoint/WorkingDir into the config
- [ ] M3 multiple layers (`--add-dir` repeated, or a tiny Dockerfile)
- [x] **M4** gzip compression + proper OCI Image Layout (`blobs/`, `index.json`, `oci-layout`) via `--format oci`
- [ ] M5 push to a registry (OCI Distribution API)
```
