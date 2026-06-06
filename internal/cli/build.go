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
	layerDirs  []string
	tag        string
	cmd        []string
	env        []string
	entrypoint []string
	workdir    string
	arch       string
	osName     string
	out        string
	format     string // "docker" (docker-archive) or "oci" (OCI Image Layout)
}

// layerSources returns the rootfs directories that become layers, ordered
// lowest -> highest: --from-dir (if set) is the base, then each --layer.
func (o buildOptions) layerSources() []string {
	var dirs []string
	if o.fromDir != "" {
		dirs = append(dirs, o.fromDir)
	}
	return append(dirs, o.layerDirs...)
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
	f.StringVar(&opts.fromDir, "from-dir", "", "base rootfs directory (the lowest layer)")
	f.StringArrayVar(&opts.layerDirs, "layer", nil, "additional rootfs directory stacked on top, lowest->highest (repeatable)")
	f.StringVar(&opts.tag, "tag", "toyimg:latest", "image tag")
	f.StringArrayVar(&opts.cmd, "cmd", nil, "container Cmd (repeatable)")
	f.StringArrayVar(&opts.env, "env", nil, "environment variable KEY=VAL (repeatable)")
	f.StringArrayVar(&opts.entrypoint, "entrypoint", nil, "Entrypoint (repeatable)")
	f.StringVar(&opts.workdir, "workdir", "", "working directory")
	f.StringVar(&opts.arch, "arch", runtime.GOARCH, "architecture")
	f.StringVar(&opts.osName, "os", "linux", "OS")
	f.StringVar(&opts.format, "format", "docker", `output format: "docker" (docker-archive) or "oci" (OCI Image Layout)`)
	f.StringVarP(&opts.out, "output", "o", "out.tar", "output tar path")

	return c
}

func runBuild(opts buildOptions) error {
	if opts.format != "docker" && opts.format != "oci" {
		return fmt.Errorf("invalid --format %q (want \"docker\" or \"oci\")", opts.format)
	}

	dirs := opts.layerSources()
	if len(dirs) == 0 {
		return fmt.Errorf("provide at least one layer via --from-dir or --layer")
	}

	// 1. Each rootfs dir -> one uncompressed layer tar + diff_id.
	//    Layers stack lowest -> highest in the order given.
	var (
		layers  []*layer.Layer
		diffIDs []string
		history []image.History
	)
	for _, d := range dirs {
		l, err := layer.FromDir(d)
		if err != nil {
			return fmt.Errorf("build layer %s: %w", d, err)
		}
		layers = append(layers, l)
		diffIDs = append(diffIDs, l.DiffID)
		history = append(history, image.History{CreatedBy: "toy-build-oci build --layer " + d})
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
			DiffIDs: diffIDs,
		},
		History: history,
	}
	cfgData, cfgHex, err := cfg.Marshal()
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	// 3. Write it out in the requested format.
	f, err := os.Create(opts.out)
	if err != nil {
		return err
	}
	defer f.Close()

	switch opts.format {
	case "docker":
		err = archive.WriteDockerArchive(f, cfgData, cfgHex, layers, []string{opts.tag})
	case "oci":
		err = archive.WriteOCIArchive(f, cfgData, cfgHex, layers, opts.tag, opts.arch, opts.osName)
	}
	if err != nil {
		return fmt.Errorf("write %s archive: %w", opts.format, err)
	}

	fmt.Printf("built %s (format: %s, %d layer(s))\n", opts.tag, opts.format, len(layers))
	for i, l := range layers {
		fmt.Printf("  layer[%d] diff_id : %s  (%s)\n", i, l.DiffID, dirs[i])
	}
	fmt.Printf("  config digest    : sha256:%s\n", cfgHex)
	fmt.Printf("  output           : %s\n", opts.out)
	if opts.format == "oci" {
		fmt.Printf("\nload with: podman load -i %s   (or: skopeo copy oci-archive:%s docker-daemon:%s)\n", opts.out, opts.out, opts.tag)
	} else {
		fmt.Printf("\nload with: docker load -i %s\n", opts.out)
	}
	return nil
}
