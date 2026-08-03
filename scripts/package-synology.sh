#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
ICON_DIR="${ROOT}/assets/icons"
VERSION=${VERSION:-0.1.0}
SPK_BUILD=${SPK_BUILD:-0001}
SPK_ARCH=${SPK_ARCH:-x86_64}
export COPYFILE_DISABLE=1

# DSM expects major, minor, micro, and build numbers, as in "0.1.0-0001".
if ! printf '%s' "${VERSION}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "VERSION must be major.minor.micro, as in 0.1.0" >&2
    exit 2
fi
if ! printf '%s' "${SPK_BUILD}" | grep -Eq '^[0-9]+$'; then
    echo "SPK_BUILD must contain only numbers" >&2
    exit 2
fi

case "${SPK_ARCH}" in
    x86_64) GOARCH=amd64 ;;
    armv8) GOARCH=arm64 ;;
    *) echo "SPK_ARCH must be x86_64 or armv8" >&2; exit 2 ;;
esac

if [ ! -d "${ROOT}/apps/dash/dist" ]; then
    echo "apps/dash/dist is missing; run 'make build-web' first" >&2
    exit 1
fi

STAGE=$(mktemp -d)
trap 'rm -rf "${STAGE}"' EXIT INT TERM
PAYLOAD="${STAGE}/payload"
PACKAGE="${STAGE}/package"
mkdir -p "${PAYLOAD}/bin" "${PAYLOAD}/web" "${PAYLOAD}/port_conf" \
    "${PACKAGE}/scripts" "${PACKAGE}/conf"

(
    cd "${ROOT}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
        go build -trimpath -ldflags="-s -w" -o "${PAYLOAD}/bin/omnisave-server" ./cmd/server
)
cp -R "${ROOT}/apps/dash/dist/." "${PAYLOAD}/web/"
cp "${ROOT}/packaging/synology/port_conf/omnisave.sc" "${PAYLOAD}/port_conf/omnisave.sc"
tar -czf "${PACKAGE}/package.tgz" -C "${PAYLOAD}" .

sed \
    -e "s/@VERSION@/${VERSION}/g" \
    -e "s/@BUILD@/${SPK_BUILD}/g" \
    -e "s/@ARCH@/${SPK_ARCH}/g" \
    "${ROOT}/packaging/synology/INFO.template" > "${PACKAGE}/INFO"
cp -R "${ROOT}/packaging/synology/scripts/." "${PACKAGE}/scripts/"
cp -R "${ROOT}/packaging/synology/conf/." "${PACKAGE}/conf/"
cp "${ICON_DIR}/omnisave-64.png" "${PACKAGE}/PACKAGE_ICON.PNG"
cp "${ICON_DIR}/omnisave-256.png" "${PACKAGE}/PACKAGE_ICON_256.PNG"
chmod 755 "${PACKAGE}/scripts/"*

mkdir -p "${ROOT}/dist"
OUTPUT="${ROOT}/dist/omnisave-${VERSION}-${SPK_BUILD}-${SPK_ARCH}.spk"
tar -cf "${OUTPUT}" -C "${PACKAGE}" \
    INFO package.tgz scripts conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG

echo "${OUTPUT}"
