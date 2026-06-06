// Command toy-build-oci is a minimal CLI that produces an OCI image
// (a docker-archive tar loadable by docker/podman) from a rootfs directory.
//
// Usage:
//
//	toy-build-oci build --from-dir ./testdata/rootfs --tag toyimg:latest \
//	    --cmd /hello -o out.tar
package main

import (
	"fmt"
	"os"

	"github.com/sakurai-ryo/toy-build-oci/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
