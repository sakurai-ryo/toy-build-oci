# Match the architecture docker runs (Docker Desktop on Apple Silicon is linux/arm64).
ARCH ?= $(shell go env GOARCH)
TAG  ?= toyimg:latest
OUT  ?= out.tar

OCI_OUT   ?= out-oci.tar
MULTI_OUT ?= out-multi.tar
MULTI_TAG ?= toymulti:latest

.PHONY: build hello image load run image-oci run-oci multi run-multi demo clean

# Build the CLI itself.
build:
	go build -o bin/toy-build-oci ./cmd/toy-build-oci

# Statically link the test "hello" binary and place it in the rootfs.
hello:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -o testdata/rootfs/hello ./hello

# Produce a docker-archive image tar containing hello.
image: build hello
	./bin/toy-build-oci build --from-dir ./testdata/rootfs --tag $(TAG) \
		--cmd /hello --arch $(ARCH) -o $(OUT)

# Load into docker.
load: image
	docker load -i $(OUT)

# Load and run (end-to-end check).
run: load
	docker run --rm $(TAG)

# Produce an OCI Image Layout tar (gzip-compressed layers) containing hello.
image-oci: build hello
	./bin/toy-build-oci build --from-dir ./testdata/rootfs --tag $(TAG) \
		--cmd /hello --arch $(ARCH) --format oci -o $(OCI_OUT)

# Load the OCI archive and run it (Docker 24+/Podman can load OCI archives).
run-oci: image-oci
	docker load -i $(OCI_OUT)
	docker run --rm $(TAG)

# Build a two-layer image: base rootfs (hello) + an overlay layer.
multi: build hello
	./bin/toy-build-oci build --from-dir ./testdata/rootfs --layer ./testdata/overlay \
		--tag $(MULTI_TAG) --cmd /hello --arch $(ARCH) -o $(MULTI_OUT)

# Load the two-layer image and show its stacked layers.
run-multi: multi
	docker load -i $(MULTI_OUT)
	docker run --rm $(MULTI_TAG)
	docker image inspect $(MULTI_TAG) --format 'layers={{len .RootFS.Layers}}'

# Run the whole flow.
demo: run

clean:
	rm -rf bin out.tar out-oci.tar out-multi.tar testdata/rootfs/hello
