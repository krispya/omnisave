VERSION ?= 0.1.0
# Default to one UTC timestamp per invocation. CI can override this with its
# monotonic run number, for example: make build-spk SPK_BUILD=1234.
SPK_BUILD ?= $(shell date -u +%Y%m%d%H%M%S)
SPK_BUILD := $(SPK_BUILD)
OCI_IMAGE ?= ghcr.io/krisbaumgartner/omnisave
OCI_PLATFORMS ?= linux/amd64,linux/arm64
TEST_GOALS = $(filter-out test,$(MAKECMDGOALS))
TEST_FILTER_PACKAGES = $(addsuffix /...,$(shell find . -type d -name '$(F)' 2>/dev/null))
TEST_GOAL_PACKAGES = $(addprefix ./,$(TEST_GOALS))
TEST_PACKAGES = $(if $(F),$(TEST_FILTER_PACKAGES),$(TEST_GOAL_PACKAGES))

install:
	pnpm install

install-client:
	GOBIN="$(HOME)/.local/bin" go install ./cmd/omnisave

build-web:
	pnpm --filter @omnisave/dash run build

build-server:
	mkdir -p bin
	go build -trimpath -o bin/omnisave-server ./cmd/server

build-client:
	go build -o bin/omnisave ./cmd/omnisave

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

build-oci:
	docker build -f Containerfile -t $(OCI_IMAGE):$(VERSION) -t $(OCI_IMAGE):latest .

push-oci:
	docker buildx build -f Containerfile --platform $(OCI_PLATFORMS) \
		-t $(OCI_IMAGE):$(VERSION) -t $(OCI_IMAGE):latest --push .

# Export image as a .tar file for manual import into Synology Container Manager.
export-oci: build-oci
	mkdir -p dist
	docker save $(OCI_IMAGE):$(VERSION) | gzip > dist/omnisave-$(VERSION)-oci.tar.gz
	@echo "Image saved to dist/omnisave-$(VERSION)-oci.tar.gz"
	@echo "Import it with Synology Container Manager's image import flow"

# ── Synology SPK ──────────────────────────────────────────────────────────────

build-spk: build-web build-spk-x86_64 build-spk-armv8
	./scripts/verify-synology-spk.sh \
		dist/omnisave-$(VERSION)-$(SPK_BUILD)-x86_64.spk \
		dist/omnisave-$(VERSION)-$(SPK_BUILD)-armv8.spk

build-spk-x86_64: build-web
	VERSION=$(VERSION) SPK_BUILD=$(SPK_BUILD) SPK_ARCH=x86_64 ./scripts/package-synology.sh

build-spk-armv8: build-web
	VERSION=$(VERSION) SPK_BUILD=$(SPK_BUILD) SPK_ARCH=armv8 ./scripts/package-synology.sh

.PHONY: install install-client build-web build-server build-client build-all test \
		build-oci push-oci export-oci build-spk build-spk-x86_64 build-spk-armv8
