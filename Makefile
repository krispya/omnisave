VERSION ?= 0.1.0
# DSM requires a numeric, increasing build number. Tying it to repository
# history also gives client and SPK builds the same identity for a commit.
BUILD ?= $(shell git rev-list --count HEAD)
BUILD := $(BUILD)
CLIENT_LDFLAGS = -X main.version=$(VERSION) -X main.buildNumber=$(BUILD)
OCI_IMAGE ?= ghcr.io/krispya/omnisave
OCI_PLATFORMS ?= linux/amd64,linux/arm64
TEST_GOALS = $(filter-out test,$(MAKECMDGOALS))
TEST_FILTER_PACKAGES = $(addsuffix /...,$(shell find . -type d -name '$(F)' 2>/dev/null))
TEST_GOAL_PACKAGES = $(addprefix ./,$(TEST_GOALS))
TEST_PACKAGES = $(if $(F),$(TEST_FILTER_PACKAGES),$(TEST_GOAL_PACKAGES))

install:
	pnpm install

install-client:
	GOBIN="$(HOME)/.local/bin" go install -ldflags="$(CLIENT_LDFLAGS)" ./cmd/omnisave

build-web:
	pnpm --filter @omnisave/dash run build

build-server:
	mkdir -p bin
	go build -trimpath -o bin/omnisave-server ./cmd/server

build-client:
	go build -trimpath -ldflags="$(CLIENT_LDFLAGS)" -o bin/omnisave ./cmd/omnisave

build-all: build-web build-server

# ── Client release archives ───────────────────────────────────────────────────

dist-client:
	VERSION=$(VERSION) BUILD=$(BUILD) ./scripts/package-client.sh
	./scripts/verify-client-archives.sh \
		dist/omnisave-$(VERSION)-linux-amd64.tar.gz \
		dist/omnisave-$(VERSION)-linux-arm64.tar.gz \
		dist/omnisave-$(VERSION)-darwin-amd64.tar.gz \
		dist/omnisave-$(VERSION)-darwin-arm64.tar.gz \
		dist/omnisave-$(VERSION)-windows-amd64.zip

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
		dist/omnisave-$(VERSION)-$(BUILD)-x86_64.spk \
		dist/omnisave-$(VERSION)-$(BUILD)-armv8.spk

build-spk-x86_64: build-web
	VERSION=$(VERSION) BUILD=$(BUILD) SPK_ARCH=x86_64 ./scripts/package-synology.sh

build-spk-armv8: build-web
	VERSION=$(VERSION) BUILD=$(BUILD) SPK_ARCH=armv8 ./scripts/package-synology.sh

.PHONY: install install-client build-web build-server build-client build-all test \
		dist-client build-oci push-oci export-oci build-spk build-spk-x86_64 \
		build-spk-armv8
