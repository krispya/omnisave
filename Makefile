VERSION ?= 1.0.0
TEST_GOALS = $(filter-out test,$(MAKECMDGOALS))
TEST_FILTER_PACKAGES = $(addsuffix /...,$(shell find . -type d -name '$(F)' 2>/dev/null))
TEST_GOAL_PACKAGES = $(addprefix ./,$(TEST_GOALS))
TEST_PACKAGES = $(if $(F),$(TEST_FILTER_PACKAGES),$(TEST_GOAL_PACKAGES))

build-server:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/omnisave-server ./cmd/server

build-client:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/omnisave-client ./cmd/client

build-client-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/omnisave-client-arm64 ./cmd/client

build-all: build-server build-client build-client-arm64

test:
	@if [ -n "$(F)" ] && [ -z "$(TEST_FILTER_PACKAGES)" ]; then \
		echo "No directory named '$(F)' found"; exit 1; \
	fi
	gotestsum --format testdox $(if $(TEST_PACKAGES),$(TEST_PACKAGES),./...)

# Allow package patterns after the test goal, for example:
# make test ./internal/omnisave/...
./%:
	@:


# ── Docker ────────────────────────────────────────────────────────────────────

docker-build:
	docker build --platform linux/amd64 -t omnisave-server:$(VERSION) -t omnisave-server:latest .

# Export image as a .tar file for manual import into Synology Container Manager.
docker-export: docker-build
	mkdir -p dist
	docker save omnisave-server:$(VERSION) | gzip > dist/omnisave-server-$(VERSION).tar.gz
	@echo "Image saved to dist/omnisave-server-$(VERSION).tar.gz"
	@echo "Import in DSM: Container Manager → Registry → Import"

# ── Synology SPK ──────────────────────────────────────────────────────────────

package-spk: build-server
	$(eval SPK_STAGE := $(shell mktemp -d))
	$(eval SPK_TARGET := $(SPK_STAGE)/target/bin)
	mkdir -p $(SPK_TARGET)

	# Binary
	cp bin/omnisave-server $(SPK_TARGET)/omnisave-server
	chmod +x $(SPK_TARGET)/omnisave-server

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

.PHONY: build-server build-client build-client-arm64 build-all test \
        docker-build docker-export package-spk
