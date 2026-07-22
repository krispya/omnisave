VERSION ?= 0.1.0
OCI_IMAGE ?= ghcr.io/krisbaumgartner/omnisave
OCI_PLATFORMS ?= linux/amd64,linux/arm64
TEST_GOALS = $(filter-out test,$(MAKECMDGOALS))
TEST_FILTER_PACKAGES = $(addsuffix /...,$(shell find . -type d -name '$(F)' 2>/dev/null))
TEST_GOAL_PACKAGES = $(addprefix ./,$(TEST_GOALS))
TEST_PACKAGES = $(if $(F),$(TEST_FILTER_PACKAGES),$(TEST_GOAL_PACKAGES))

install:
	pnpm install

build-web:
	pnpm --filter @omnisave/dash run build

build-server:
	mkdir -p bin
	go build -trimpath -o bin/omnisave-server ./cmd/server

build-client:
	go build -o bin/omnisave-client ./cmd/client

build-all: build-web build-server

test:
	@if [ -n "$(F)" ] && [ -z "$(TEST_FILTER_PACKAGES)" ]; then \
		echo "No directory named '$(F)' found"; exit 1; \
	fi
	go tool gotestsum --format testdox $(if $(TEST_PACKAGES),$(TEST_PACKAGES),./...)

# Allow package patterns after the test goal, for example:
# make test ./internal/omnisave/...
./%:
	@:


# ── OCI image ─────────────────────────────────────────────────────────────────

oci-build docker-build:
	docker build -f Containerfile -t $(OCI_IMAGE):$(VERSION) -t $(OCI_IMAGE):latest .

oci-push:
	docker buildx build -f Containerfile --platform $(OCI_PLATFORMS) \
		-t $(OCI_IMAGE):$(VERSION) -t $(OCI_IMAGE):latest --push .

# Export image as a .tar file for manual import into Synology Container Manager.
docker-export: oci-build
	mkdir -p dist
	docker save $(OCI_IMAGE):$(VERSION) | gzip > dist/omnisave-$(VERSION)-oci.tar.gz
	@echo "Image saved to dist/omnisave-$(VERSION)-oci.tar.gz"
	@echo "Import it with Synology Container Manager's image import flow"

# ── Synology SPK ──────────────────────────────────────────────────────────────

package-spk: build-web build-server
	$(eval SPK_STAGE := $(shell mktemp -d))
	$(eval SPK_TARGET := $(SPK_STAGE)/target/bin)
	mkdir -p $(SPK_TARGET)

	# Binary
	cp bin/omnisave-server $(SPK_TARGET)/omnisave-server
	chmod +x $(SPK_TARGET)/omnisave-server
	cp -r web/dist $(SPK_STAGE)/target/web

	# Inner package.tgz
	tar -czf $(SPK_STAGE)/package.tgz -C $(SPK_STAGE) target

	# INFO + scripts
	cp packaging/synology/INFO $(SPK_STAGE)/INFO
	cp -r packaging/synology/scripts $(SPK_STAGE)/scripts
	chmod +x $(SPK_STAGE)/scripts/*

	# Assemble .spk (uncompressed tar)
	mkdir -p dist
	tar -cf dist/omnisave-$(VERSION)-x86_64.spk \
	    -C $(SPK_STAGE) INFO package.tgz scripts

	rm -rf $(SPK_STAGE)
	@echo "SPK written to dist/omnisave-$(VERSION)-x86_64.spk"
	@echo "Install in DSM: Package Center → Manual Install"

.PHONY: install build-web build-server build-client build-all test \
		oci-build oci-push docker-build docker-export package-spk
