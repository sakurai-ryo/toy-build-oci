// Command hello is a tiny program used for verification.
// Linking it statically with CGO_ENABLED=0 removes the need for shared
// libraries, so dropping a single file into a rootfs yields a runnable
// `FROM scratch`-equivalent image.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("hello from toy-build-oci")
	if len(os.Args) > 1 {
		fmt.Println("args:", os.Args[1:])
	}
}
