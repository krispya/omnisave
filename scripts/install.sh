#!/bin/sh
set -eu

# Installs the omnisave client on macOS, Linux, and SteamOS.
#
#   curl -fsSL https://raw.githubusercontent.com/krispya/omnisave/main/scripts/install.sh | sh
#
# This script is run by piping it into a shell, so it never reads standard
# input: stdin is the script itself, and a prompt would consume the rest of it.
# Everything configurable is an environment variable instead:
#
#   OMNISAVE_VERSION         version to install, without the leading v (default: latest)
#   OMNISAVE_BIN             directory to install into (default: ~/.local/bin)
#   OMNISAVE_NO_MODIFY_PATH  set to any value to leave shell startup files alone
#   OMNISAVE_BASE_URL        directory URL holding the archives, for installing
#                            from somewhere other than the GitHub release;
#                            requires OMNISAVE_VERSION

REPO=${OMNISAVE_REPO:-krispya/omnisave}
INSTALL_DIR=${OMNISAVE_BIN:-${HOME}/.local/bin}
VERSION=${OMNISAVE_VERSION:-latest}
BASE_URL=${OMNISAVE_BASE_URL:-}

abort() {
    echo "omnisave: $1" >&2
    exit 1
}

# ── Environment ───────────────────────────────────────────────────────────────

if command -v curl >/dev/null 2>&1; then
    DOWNLOADER=curl
elif command -v wget >/dev/null 2>&1; then
    DOWNLOADER=wget
else
    abort "curl or wget is required"
fi

fetch() {
    # fetch URL DESTINATION
    if [ "${DOWNLOADER}" = curl ]; then
        curl -fsSL "$1" -o "$2"
    else
        wget -qO "$2" "$1"
    fi
}

case "$(uname -s)" in
    Linux) GOOS=linux ;;
    Darwin) GOOS=darwin ;;
    *) abort "unsupported operating system $(uname -s); Windows uses install.ps1" ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) GOARCH=amd64 ;;
    aarch64 | arm64) GOARCH=arm64 ;;
    *) abort "unsupported architecture $(uname -m)" ;;
esac

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        echo ""
    fi
}

# ── Resolve the release ───────────────────────────────────────────────────────

WORK=$(mktemp -d)
trap 'rm -rf "${WORK}"' EXIT INT TERM

if [ -n "${BASE_URL}" ] && [ "${VERSION}" = latest ]; then
    abort "set OMNISAVE_VERSION when OMNISAVE_BASE_URL is set: only a GitHub release can name its own latest version"
fi

if [ "${VERSION}" = latest ]; then
    # The releases API names the newest published tag. Pin OMNISAVE_VERSION to
    # skip this request entirely. The response lands in a file rather than a
    # pipeline so a failed download is what aborts, not the parser downstream.
    fetch "https://api.github.com/repos/${REPO}/releases/latest" "${WORK}/release.json" ||
        abort "could not reach the GitHub releases API; set OMNISAVE_VERSION to install a known version"
    TAG=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${WORK}/release.json" | head -1)
    [ -n "${TAG}" ] || abort "no published release found for ${REPO}"
    VERSION=${TAG#v}
fi

ARCHIVE="omnisave-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
CHECKSUMS="omnisave-${VERSION}-checksums.txt"
if [ -z "${BASE_URL}" ]; then
    BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"
fi

# ── Download and verify ───────────────────────────────────────────────────────

echo "Installing omnisave ${VERSION} for ${GOOS}/${GOARCH}"

fetch "${BASE_URL}/${ARCHIVE}" "${WORK}/${ARCHIVE}" ||
    abort "could not download ${ARCHIVE}; this release may not publish ${GOOS}/${GOARCH}"
fetch "${BASE_URL}/${CHECKSUMS}" "${WORK}/${CHECKSUMS}" ||
    abort "could not download ${CHECKSUMS}"

EXPECTED=$(awk -v name="${ARCHIVE}" '$2 == name { print $1 }' "${WORK}/${CHECKSUMS}")
[ -n "${EXPECTED}" ] || abort "${ARCHIVE} is not listed in ${CHECKSUMS}"

ACTUAL=$(sha256_of "${WORK}/${ARCHIVE}")
if [ -z "${ACTUAL}" ]; then
    abort "sha256sum or shasum is required to verify the download"
fi
if [ "${ACTUAL}" != "${EXPECTED}" ]; then
    abort "checksum mismatch for ${ARCHIVE}; refusing to install"
fi

tar -xzf "${WORK}/${ARCHIVE}" -C "${WORK}"
[ -f "${WORK}/omnisave" ] || abort "${ARCHIVE} did not contain an omnisave binary"

# ── Install ───────────────────────────────────────────────────────────────────

mkdir -p "${INSTALL_DIR}" || abort "could not create ${INSTALL_DIR}"
[ -w "${INSTALL_DIR}" ] || abort "${INSTALL_DIR} is not writable"

# Move within the destination directory so the replacement is atomic: a client
# already running from this path keeps its open binary, and no reader ever
# sees a partially written file.
STAGED="${INSTALL_DIR}/.omnisave.$$"
cp "${WORK}/omnisave" "${STAGED}"
chmod 755 "${STAGED}"
mv -f "${STAGED}" "${INSTALL_DIR}/omnisave"

echo "Installed ${INSTALL_DIR}/omnisave"

# ── PATH ──────────────────────────────────────────────────────────────────────

# SteamOS and most Linux distributions do not put ~/.local/bin on PATH for a
# non-login shell, so an install that stops here reads as "command not found".
case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) exit 0 ;;
esac

if [ -n "${OMNISAVE_NO_MODIFY_PATH:-}" ]; then
    echo
    echo "${INSTALL_DIR} is not on your PATH. Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    exit 0
fi

SHELL_NAME=$(basename "${SHELL:-/bin/sh}")
case "${SHELL_NAME}" in
    zsh) PROFILE="${ZDOTDIR:-${HOME}}/.zshrc" ;;
    bash)
        # macOS Terminal opens login shells, which read .bash_profile; Linux
        # terminals open interactive non-login shells, which read .bashrc.
        if [ "${GOOS}" = darwin ]; then
            PROFILE="${HOME}/.bash_profile"
        else
            PROFILE="${HOME}/.bashrc"
        fi
        ;;
    fish) PROFILE="${HOME}/.config/fish/config.fish" ;;
    *) PROFILE="" ;;
esac

if [ -z "${PROFILE}" ]; then
    echo
    echo "${INSTALL_DIR} is not on your PATH. Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    exit 0
fi

MARKER="# added by omnisave installer"
if [ -f "${PROFILE}" ] && grep -qF "${MARKER}" "${PROFILE}"; then
    echo
    echo "${PROFILE} already adds ${INSTALL_DIR} to PATH; open a new terminal to pick it up."
    exit 0
fi

mkdir -p "$(dirname "${PROFILE}")"
{
    echo ""
    echo "${MARKER}"
    if [ "${SHELL_NAME}" = fish ]; then
        echo "fish_add_path \"${INSTALL_DIR}\""
    else
        echo "export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
} >> "${PROFILE}"

echo
echo "Added ${INSTALL_DIR} to PATH in ${PROFILE}"
echo "Open a new terminal, or run: export PATH=\"${INSTALL_DIR}:\$PATH\""
