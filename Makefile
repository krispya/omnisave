# Local artifacts keep the nearest release tag's semantic version while the
# build number advances on each subsequent commit. Release builds still pass
# VERSION explicitly from the tag that triggered the workflow.
VERSION_TAG := $(shell git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null)
VERSION ?= $(if $(VERSION_TAG),$(patsubst v%,%,$(VERSION_TAG)),0.1.0)
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

# Refresh the pruned Ludusavi manifest embedded in the client binary.
refresh-save-profiles:
	go run ./scripts/ludusavi-manifest

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

# Build every downloadable artifact for one version. OCI images are published
# separately because a multi-platform image has no single local archive.
dist-release: dist-client build-spk

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

# OCI_BUILD_ARGS lets CI pass cache flags without affecting local builds.
push-oci:
	docker buildx build -f Containerfile --platform $(OCI_PLATFORMS) $(OCI_BUILD_ARGS) \
		-t $(OCI_IMAGE):$(VERSION) -t $(OCI_IMAGE):latest --push .

# ── Synology SPK ──────────────────────────────────────────────────────────────

build-spk: build-web build-spk-x86_64 build-spk-armv8
	./scripts/verify-synology-spk.sh \
		dist/omnisave-server-$(VERSION)-x86_64.spk \
		dist/omnisave-server-$(VERSION)-armv8.spk

build-spk-x86_64: build-web
	VERSION=$(VERSION) BUILD=$(BUILD) SPK_ARCH=x86_64 ./scripts/package-synology.sh

build-spk-armv8: build-web
	VERSION=$(VERSION) BUILD=$(BUILD) SPK_ARCH=armv8 ./scripts/package-synology.sh

.PHONY: install refresh-save-profiles install-client build-web build-server build-client build-all test \
		dist-client dist-release build-oci push-oci build-spk build-spk-x86_64 \
		build-spk-armv8
