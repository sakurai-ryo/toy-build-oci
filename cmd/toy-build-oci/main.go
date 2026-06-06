// Command toy-build-oci is a minimal CLI that produces an OCI image
// (a docker-archive tar loadable by docker/podman) from a rootfs directory.
//
// Usage:
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
	fmt.Fprint(os.Stderr, `toy-build-oci - a minimal OCI image builder

Usage:
  toy-build-oci build [flags]

build flags:
  --from-dir DIR     rootfs directory to turn into a layer (required)
  --tag NAME:TAG     image tag (default: toyimg:latest)
  --cmd ARG          container Cmd (repeatable)
  --env KEY=VAL      environment variable (repeatable)
  --entrypoint ARG   Entrypoint (repeatable)
  --workdir DIR      working directory
  --arch ARCH        architecture (default: host GOARCH)
  --os OS            OS (default: linux)
  -o FILE            output tar path (default: out.tar)
`)
}

// stringSlice lets the same flag be given repeatedly (--cmd a --cmd b).
type stringSlice []string

func (s *stringSlice) String() string     { return fmt.Sprint([]string(*s)) }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	var (
		fromDir    = fs.String("from-dir", "", "rootfs directory to turn into a layer")
		tag        = fs.String("tag", "toyimg:latest", "image tag")
		workdir    = fs.String("workdir", "", "working directory")
		arch       = fs.String("arch", runtime.GOARCH, "architecture")
		osName     = fs.String("os", "linux", "OS")
		out        = fs.String("o", "out.tar", "output tar path")
		cmd        stringSlice
		env        stringSlice
		entrypoint stringSlice
	)
	fs.Var(&cmd, "cmd", "container Cmd (repeatable)")
	fs.Var(&env, "env", "environment variable KEY=VAL (repeatable)")
	fs.Var(&entrypoint, "entrypoint", "Entrypoint (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fromDir == "" {
		return fmt.Errorf("--from-dir is required")
	}

	// 1. rootfs -> uncompressed layer tar + diff_id
	l, err := layer.FromDir(*fromDir)
	if err != nil {
		return fmt.Errorf("build layer: %w", err)
	}

	// 2. Assemble the Image Config.
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
		return fmt.Errorf("build config: %w", err)
	}

	// 3. Write it out as a docker-archive tar.
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := archive.WriteDockerArchive(f, cfgData, cfgHex, []*layer.Layer{l}, []string{*tag}); err != nil {
		return fmt.Errorf("write tar: %w", err)
	}

	fmt.Printf("built %s\n", *tag)
	fmt.Printf("  layer diff_id : %s\n", l.DiffID)
	fmt.Printf("  config digest : sha256:%s\n", cfgHex)
	fmt.Printf("  output        : %s\n", *out)
	fmt.Printf("\nload with: docker load -i %s\n", *out)
	return nil
}
