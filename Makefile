# 検証用に docker が動くアーキテクチャへ合わせる(Docker Desktop on Apple Silicon は linux/arm64)。
ARCH ?= $(shell go env GOARCH)
TAG  ?= toyimg:latest
OUT  ?= out.tar

.PHONY: build hello image load run demo clean

# CLI本体をビルド
build:
	go build -o bin/toy-build-oci ./cmd/toy-build-oci

# 検証用helloを静的リンクして rootfs に配置
hello:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -o testdata/rootfs/hello ./hello

# helloを含むイメージtarを生成
image: build hello
	./bin/toy-build-oci build --from-dir ./testdata/rootfs --tag $(TAG) \
		--cmd /hello --arch $(ARCH) -o $(OUT)

# docker にロード
load: image
	docker load -i $(OUT)

# ロードして実行(end-to-end検証)
run: load
	docker run --rm $(TAG)

# 一連の流れをまとめて実行
demo: run

clean:
	rm -rf bin out.tar testdata/rootfs/hello
