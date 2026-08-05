#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
    echo "usage: verify-synology-spk.sh package.spk [...]" >&2
    exit 2
fi

WORK=$(mktemp -d)
trap 'rm -rf "${WORK}"' EXIT INT TERM

for SPK in "$@"; do
    NAME=$(basename "${SPK}" .spk)
    ROOT="${WORK}/${NAME}"
    PAYLOAD="${ROOT}/payload"
    mkdir -p "${ROOT}" "${PAYLOAD}"
    tar -xf "${SPK}" -C "${ROOT}"

    for REQUIRED in INFO package.tgz scripts conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG; do
        if [ ! -e "${ROOT}/${REQUIRED}" ]; then
            echo "${SPK}: missing ${REQUIRED}" >&2
            exit 1
        fi
    done

    # DSM 7 rejects a package missing any of these INFO fields.
    for FIELD in package version os_min_ver description arch maintainer dsmuidir dsmappname; do
        if ! grep -q "^${FIELD}=" "${ROOT}/INFO"; then
            echo "${SPK}: INFO is missing ${FIELD}" >&2
            exit 1
        fi
    done

    # DSM 7 replaced DSM 6's 72x72 package icon with a 64x64 one.
    if ! file "${ROOT}/PACKAGE_ICON.PNG" | grep -q ', 64 x 64,'; then
        echo "${SPK}: PACKAGE_ICON.PNG must be 64x64" >&2
        exit 1
    fi
    if ! file "${ROOT}/PACKAGE_ICON_256.PNG" | grep -q ', 256 x 256,'; then
        echo "${SPK}: PACKAGE_ICON_256.PNG must be 256x256" >&2
        exit 1
    fi

    jq empty "${ROOT}/conf/privilege" "${ROOT}/conf/resource"
    if ! jq -e '
        .defaults["run-as"] == "package" and
        ([.. | strings] | index("root") == null)
    ' "${ROOT}/conf/privilege" >/dev/null; then
        echo "${SPK}: conf/privilege must run only as the package user" >&2
        exit 1
    fi
    for SCRIPT in "${ROOT}/scripts/"*; do
        sh -n "${SCRIPT}"
        if [ ! -x "${SCRIPT}" ]; then
            echo "${SPK}: ${SCRIPT} is not executable" >&2
            exit 1
        fi
    done
    for ACTION in prestart prestop; do
        if ! "${ROOT}/scripts/start-stop-status" "${ACTION}"; then
            echo "${SPK}: start-stop-status rejects ${ACTION}" >&2
            exit 1
        fi
    done

    tar -xzf "${ROOT}/package.tgz" -C "${PAYLOAD}"
    for REQUIRED in bin/omnisave-server web/index.html port_conf/omnisave.sc ui/config; do
        if [ ! -e "${PAYLOAD}/${REQUIRED}" ]; then
            echo "${SPK}: payload is missing ${REQUIRED}" >&2
            exit 1
        fi
    done
    if [ -e "${PAYLOAD}/target" ]; then
        echo "${SPK}: package.tgz must not contain a nested target directory" >&2
        exit 1
    fi

    jq empty "${PAYLOAD}/ui/config"
    if ! grep -q '^dsmuidir="ui"$' "${ROOT}/INFO"; then
        echo "${SPK}: INFO must expose the DSM application from ui" >&2
        exit 1
    fi
    APP_NAME=$(sed -n 's/^dsmappname="\([^"]*\)"$/\1/p' "${ROOT}/INFO")
    if ! jq -e --arg APP_NAME "${APP_NAME}" '
        .[".url"][$APP_NAME] |
        .type == "url" and
        .title == "Omnisave" and
        .icon == "images/omnisave-{0}.png" and
        .protocol == "http" and
        .port == "8080" and
        .url == "/" and
        .allUsers == true
    ' "${PAYLOAD}/ui/config" >/dev/null; then
        echo "${SPK}: ui/config does not register the Omnisave DSM application" >&2
        exit 1
    fi
    for SIZE in 16 24 32 48 64 72 256; do
        ICON="${PAYLOAD}/ui/images/omnisave-${SIZE}.png"
        if ! file "${ICON}" | grep -q ", ${SIZE} x ${SIZE},"; then
            echo "${SPK}: DSM application icon must be ${SIZE}x${SIZE}: ${ICON}" >&2
            exit 1
        fi
    done

    case "${SPK}" in
        *-x86_64.spk) file "${PAYLOAD}/bin/omnisave-server" | grep -q 'x86-64' ;;
        *-armv8.spk) file "${PAYLOAD}/bin/omnisave-server" | grep -Eq 'arm64|aarch64' ;;
        *) echo "${SPK}: architecture is not present in the filename" >&2; exit 1 ;;
    esac

    echo "verified ${SPK}"
done
