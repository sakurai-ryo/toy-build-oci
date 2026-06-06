// Command hello は検証用の最小プログラム。
// CGO_ENABLED=0 で静的リンクすれば共有ライブラリ不要になり、
// rootfs に1ファイル置くだけで `FROM scratch` 相当のイメージが動く。
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
