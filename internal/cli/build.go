package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/sakurai-ryo/toy-build-oci/internal/archive"
	"github.com/sakurai-ryo/toy-build-oci/internal/image"
	"github.com/sakurai-ryo/toy-build-oci/internal/layer"
)

// buildOptions holds the parsed flags for the build command.
type buildOptions struct {
	fromDir    string
	tag        string
	cmd        []string
	env        []string
	entrypoint []string
	workdir    string
	arch       string
	osName     string
	out        string
}

func newBuildCmd() *cobra.Command {
	opts := buildOptions{}

	c := &cobra.Command{
		Use:   "build --from-dir DIR [flags]",
		Short: "Build an OCI image (docker-archive tar) from a rootfs directory",
		Example: "  toy-build-oci build --from-dir ./testdata/rootfs --tag toyimg:latest \\\n" +
			"      --cmd /hello -o out.tar",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBuild(opts)
		},
	}

	f := c.Flags()
	f.StringVar(&opts.fromDir, "from-dir", "", "rootfs directory to turn into a layer (required)")
	f.StringVar(&opts.tag, "tag", "toyimg:latest", "image tag")
	f.StringArrayVar(&opts.cmd, "cmd", nil, "container Cmd (repeatable)")
	f.StringArrayVar(&opts.env, "env", nil, "environment variable KEY=VAL (repeatable)")
	f.StringArrayVar(&opts.entrypoint, "entrypoint", nil, "Entrypoint (repeatable)")
	f.StringVar(&opts.workdir, "workdir", "", "working directory")
	f.StringVar(&opts.arch, "arch", runtime.GOARCH, "architecture")
	f.StringVar(&opts.osName, "os", "linux", "OS")
	f.StringVarP(&opts.out, "output", "o", "out.tar", "output tar path")

	_ = c.MarkFlagRequired("from-dir")

	return c
}

func runBuild(opts buildOptions) error {
	// 1. rootfs -> uncompressed layer tar + diff_id
	l, err := layer.FromDir(opts.fromDir)
	if err != nil {
		return fmt.Errorf("build layer: %w", err)
	}

	// 2. Assemble the Image Config.
	cfg := &image.Config{
		Architecture: opts.arch,
		OS:           opts.osName,
		Config: image.RunConfig{
			Env:        opts.env,
			Cmd:        opts.cmd,
			Entrypoint: opts.entrypoint,
			WorkingDir: opts.workdir,
		},
		RootFS: image.RootFS{
			Type:    "layers",
			DiffIDs: []string{l.DiffID},
		},
		History: []image.History{
			{CreatedBy: "toy-build-oci build --from-dir " + opts.fromDir},
		},
	}
	cfgData, cfgHex, err := cfg.Marshal()
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	// 3. Write it out as a docker-archive tar.
	f, err := os.Create(opts.out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := archive.WriteDockerArchive(f, cfgData, cfgHex, []*layer.Layer{l}, []string{opts.tag}); err != nil {
		return fmt.Errorf("write tar: %w", err)
	}

	fmt.Printf("built %s\n", opts.tag)
	fmt.Printf("  layer diff_id : %s\n", l.DiffID)
	fmt.Printf("  config digest : sha256:%s\n", cfgHex)
	fmt.Printf("  output        : %s\n", opts.out)
	fmt.Printf("\nload with: docker load -i %s\n", opts.out)
	return nil
}
