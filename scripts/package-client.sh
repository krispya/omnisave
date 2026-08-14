#!/bin/sh
set -eu

# Builds the client for every published platform and writes one archive per
# platform into dist/, beside a checksum file the installers verify against.

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
VERSION=${VERSION:-0.1.0}
# Keep direct script invocations aligned with Makefile builds.
BUILD=${BUILD:-$(git -C "${ROOT}" rev-list --count HEAD 2>/dev/null || printf '0001')}
export COPYFILE_DISABLE=1

# The platforms an omnisave release supports. Windows takes .zip because its
# installer runs in PowerShell, which expands zip archives without extra tools.
PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"

if ! printf '%s' "${VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "VERSION must be major.minor.micro, as in 0.1.0" >&2
    exit 2
fi
if ! printf '%s' "${BUILD}" | grep -Eq '^[0-9]+$'; then
    echo "BUILD must contain only numbers" >&2
    exit 2
fi

# macOS ships shasum; Linux ships sha256sum. Releases are cut on both.
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

STAGE=$(mktemp -d)
trap 'rm -rf "${STAGE}"' EXIT INT TERM

DIST="${ROOT}/dist"
mkdir -p "${DIST}"

CHECKSUMS="${DIST}/omnisave-${VERSION}-checksums.txt"
: > "${CHECKSUMS}"

build_platform() {
    GOOS=${1%/*}
    GOARCH=${1#*/}

    BINARY=omnisave
    if [ "${GOOS}" = "windows" ]; then
        BINARY=omnisave.exe
    fi

    PAYLOAD="${STAGE}/${GOOS}-${GOARCH}"
    mkdir -p "${PAYLOAD}"

    (
        cd "${ROOT}"
        CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
            go build -trimpath \
            -ldflags="-s -w -X main.version=${VERSION} -X main.buildNumber=${BUILD}" \
            -o "${PAYLOAD}/${BINARY}" ./cmd/omnisave
    )
    chmod 755 "${PAYLOAD}/${BINARY}"
    # The license travels with the binary: ISC asks for the notice to appear
    # in every copy, and an archive is one.
    cp "${ROOT}/LICENSE" "${PAYLOAD}/LICENSE"
}

# The cross-compiles are independent, so they run concurrently. Archiving
# stays serial below so the checksum file keeps a stable platform order.
PIDS=
for PLATFORM in ${PLATFORMS}; do
    build_platform "${PLATFORM}" &
    PIDS="${PIDS} $!"
done
for PID in ${PIDS}; do
    wait "${PID}"
done

for PLATFORM in ${PLATFORMS}; do
    GOOS=${PLATFORM%/*}
    GOARCH=${PLATFORM#*/}

    BINARY=omnisave
    if [ "${GOOS}" = "windows" ]; then
        BINARY=omnisave.exe
    fi
    PAYLOAD="${STAGE}/${GOOS}-${GOARCH}"

    # The version is enough to name a release archive: one release per version,
    # with the build number carried inside the binary rather than the filename.
    BASE="omnisave-${VERSION}-${GOOS}-${GOARCH}"
    if [ "${GOOS}" = "windows" ]; then
        ARCHIVE="${DIST}/${BASE}.zip"
        rm -f "${ARCHIVE}"
        (cd "${PAYLOAD}" && zip -q -X "${ARCHIVE}" "${BINARY}" LICENSE)
    else
        ARCHIVE="${DIST}/${BASE}.tar.gz"
        tar -czf "${ARCHIVE}" -C "${PAYLOAD}" "${BINARY}" LICENSE
    fi

    printf '%s  %s\n' "$(sha256_of "${ARCHIVE}")" "$(basename "${ARCHIVE}")" >> "${CHECKSUMS}"
    echo "${ARCHIVE}"
done

echo "${CHECKSUMS}"
