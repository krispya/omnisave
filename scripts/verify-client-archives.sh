#!/bin/sh
set -eu

# Checks that release archives hold what the installers expect: one client
# binary at the archive root, built for the platform its filename claims, and
# matching the digest recorded beside it.

if [ "$#" -eq 0 ]; then
    echo "usage: verify-client-archives.sh archive [...]" >&2
    exit 2
fi

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

WORK=$(mktemp -d)
trap 'rm -rf "${WORK}"' EXIT INT TERM

for ARCHIVE in "$@"; do
    FILENAME=$(basename "${ARCHIVE}")
    case "${FILENAME}" in
        omnisave-*-*-*.tar.gz) NAME=${FILENAME%.tar.gz} ;;
        omnisave-*-*-*.zip) NAME=${FILENAME%.zip} ;;
        *) echo "${ARCHIVE}: not a release archive name" >&2; exit 1 ;;
    esac

    # omnisave-<version>-<os>-<arch>
    GOARCH=${NAME##*-}
    REST=${NAME%-*}
    GOOS=${REST##*-}
    VERSION=${REST%-*}
    VERSION=${VERSION#omnisave-}

    BINARY=omnisave
    if [ "${GOOS}" = "windows" ]; then
        BINARY=omnisave.exe
    fi

    ROOT="${WORK}/${NAME}"
    mkdir -p "${ROOT}"
    case "${FILENAME}" in
        *.tar.gz) tar -xzf "${ARCHIVE}" -C "${ROOT}" ;;
        *.zip) unzip -q "${ARCHIVE}" -d "${ROOT}" ;;
    esac

    if [ ! -f "${ROOT}/${BINARY}" ]; then
        echo "${ARCHIVE}: missing ${BINARY} at the archive root" >&2
        exit 1
    fi
    if [ ! -x "${ROOT}/${BINARY}" ]; then
        echo "${ARCHIVE}: ${BINARY} is not executable" >&2
        exit 1
    fi
    if [ ! -f "${ROOT}/LICENSE" ]; then
        echo "${ARCHIVE}: missing LICENSE" >&2
        exit 1
    fi
    if [ ! -f "${ROOT}/THIRD_PARTY_NOTICES" ]; then
        echo "${ARCHIVE}: missing THIRD_PARTY_NOTICES" >&2
        exit 1
    fi
    # The installers unpack the archive whole and install the binary alone, so
    # anything else in here is dead weight a reader would have to explain.
    ENTRIES=$(find "${ROOT}" -mindepth 1 | wc -l | tr -d ' ')
    if [ "${ENTRIES}" != "3" ]; then
        echo "${ARCHIVE}: archive must contain only ${BINARY}, LICENSE, and THIRD_PARTY_NOTICES" >&2
        exit 1
    fi

    DESCRIPTION=$(file "${ROOT}/${BINARY}")
    case "${GOOS}" in
        linux) EXPECTED_OS='ELF' ;;
        darwin) EXPECTED_OS='Mach-O' ;;
        windows) EXPECTED_OS='PE32+' ;;
        *) echo "${ARCHIVE}: unknown platform ${GOOS}" >&2; exit 1 ;;
    esac
    case "${GOARCH}" in
        amd64) EXPECTED_ARCH='x86-64|x86_64' ;;
        arm64) EXPECTED_ARCH='arm64|aarch64' ;;
        *) echo "${ARCHIVE}: unknown architecture ${GOARCH}" >&2; exit 1 ;;
    esac
    if ! printf '%s' "${DESCRIPTION}" | grep -q "${EXPECTED_OS}"; then
        echo "${ARCHIVE}: ${BINARY} is not a ${GOOS} binary: ${DESCRIPTION}" >&2
        exit 1
    fi
    if ! printf '%s' "${DESCRIPTION}" | grep -Eq "${EXPECTED_ARCH}"; then
        echo "${ARCHIVE}: ${BINARY} is not ${GOARCH}: ${DESCRIPTION}" >&2
        exit 1
    fi

    # The installers refuse a download whose digest is not recorded, so a
    # release whose checksum file disagrees with its archives cannot install.
    CHECKSUMS=$(dirname "${ARCHIVE}")/omnisave-${VERSION}-checksums.txt
    if [ ! -f "${CHECKSUMS}" ]; then
        echo "${ARCHIVE}: no checksum file at ${CHECKSUMS}" >&2
        exit 1
    fi
    RECORDED=$(awk -v name="${FILENAME}" '$2 == name { print $1 }' "${CHECKSUMS}")
    if [ -z "${RECORDED}" ]; then
        echo "${ARCHIVE}: ${FILENAME} is missing from ${CHECKSUMS}" >&2
        exit 1
    fi
    if [ "${RECORDED}" != "$(sha256_of "${ARCHIVE}")" ]; then
        echo "${ARCHIVE}: digest does not match ${CHECKSUMS}" >&2
        exit 1
    fi

    echo "verified ${ARCHIVE}"
done
