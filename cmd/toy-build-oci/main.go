// Command toy-build-oci は rootfs ディレクトリから
// docker/podman load 可能な OCI イメージ(docker-archive tar)を生成する
// 最小の CLI。
//
// 使い方:
//
//	toy-build-oci build --from-dir ./testdata/rootfs --tag toyimg:latest \
//	    --cmd /hello -o out.tar
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/sakurai-ryo/toy-build-oci/internal/archive"
	"github.com/sakurai-ryo/toy-build-oci/internal/image"
	"github.com/sakurai-ryo/toy-build-oci/internal/layer"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		if err := runBuild(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `toy-build-oci - 最小のOCIイメージビルダー

使い方:
  toy-build-oci build [flags]

build flags:
  --from-dir DIR     レイヤーにするrootfsディレクトリ (必須)
  --tag NAME:TAG     イメージのタグ (default: toyimg:latest)
  --cmd ARG          コンテナのCmd (繰り返し指定可)
  --env KEY=VAL      環境変数 (繰り返し指定可)
  --entrypoint ARG   Entrypoint (繰り返し指定可)
  --workdir DIR      作業ディレクトリ
  --arch ARCH        アーキテクチャ (default: 実行環境のGOARCH)
  --os OS            OS (default: linux)
  -o FILE            出力tarのパス (default: out.tar)
`)
}

// stringSlice は同じフラグを繰り返し指定できるようにする(--cmd a --cmd b)。
type stringSlice []string

func (s *stringSlice) String() string     { return fmt.Sprint([]string(*s)) }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	var (
		fromDir    = fs.String("from-dir", "", "レイヤーにするrootfsディレクトリ")
		tag        = fs.String("tag", "toyimg:latest", "イメージのタグ")
		workdir    = fs.String("workdir", "", "作業ディレクトリ")
		arch       = fs.String("arch", runtime.GOARCH, "アーキテクチャ")
		osName     = fs.String("os", "linux", "OS")
		out        = fs.String("o", "out.tar", "出力tarのパス")
		cmd        stringSlice
		env        stringSlice
		entrypoint stringSlice
	)
	fs.Var(&cmd, "cmd", "コンテナのCmd(繰り返し可)")
	fs.Var(&env, "env", "環境変数 KEY=VAL(繰り返し可)")
	fs.Var(&entrypoint, "entrypoint", "Entrypoint(繰り返し可)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fromDir == "" {
		return fmt.Errorf("--from-dir は必須です")
	}

	// 1. rootfs → 非圧縮レイヤーtar + diff_id
	l, err := layer.FromDir(*fromDir)
	if err != nil {
		return fmt.Errorf("レイヤー生成: %w", err)
	}

	// 2. Image Config を組み立てる
	cfg := &image.Config{
		Architecture: *arch,
		OS:           *osName,
		Config: image.RunConfig{
			Env:        env,
			Cmd:        cmd,
			Entrypoint: entrypoint,
			WorkingDir: *workdir,
		},
		RootFS: image.RootFS{
			Type:    "layers",
			DiffIDs: []string{l.DiffID},
		},
		History: []image.History{
			{CreatedBy: "toy-build-oci build --from-dir " + *fromDir},
		},
	}
	cfgData, cfgHex, err := cfg.Marshal()
	if err != nil {
		return fmt.Errorf("config生成: %w", err)
	}

	// 3. docker-archive tar として書き出す
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := archive.WriteDockerArchive(f, cfgData, cfgHex, []*layer.Layer{l}, []string{*tag}); err != nil {
		return fmt.Errorf("tar書き出し: %w", err)
	}

	fmt.Printf("built %s\n", *tag)
	fmt.Printf("  layer diff_id : %s\n", l.DiffID)
	fmt.Printf("  config digest : sha256:%s\n", cfgHex)
	fmt.Printf("  output        : %s\n", *out)
	fmt.Printf("\n読み込み: docker load -i %s\n", *out)
	return nil
}
