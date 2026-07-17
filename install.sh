#!/bin/bash

OS=$(uname -s)
if [ "$OS" != "Linux" ] && [ "$OS" != "Darwin" ]; then
    echo "This script only supports Linux, Android (Termux), and macOS platforms."
    exit 1
fi

OS_LOWER=$(echo "$OS" | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$OS_LOWER" = "darwin" ]; then
    case "$ARCH" in
        arm64)  ARCH="arm64" ;;
        x86_64) ARCH="amd64" ;;
        *)      echo "Unsupported macOS architecture: $ARCH" && exit 1 ;;
    esac
else
    case "$ARCH" in
        aarch64|arm64) ARCH="arm64" ;;
        armv7*|armv8*) ARCH="arm" ;;
        x86_64)        ARCH="amd64" ;;
        i386|i686)     ARCH="386" ;;
        *)             echo "Unsupported Linux architecture: $ARCH" && exit 1 ;;
    esac
fi

if [ "$OS_LOWER" = "darwin" ]; then
    EXT="zip"
else
    EXT="tar.gz"
fi

BINARY="wizard"
INSTALL_DIR="bpb-wizard"
ARCHIVE="BPB-Wizard-${OS_LOWER}-${ARCH}.${EXT}"
WORKER_URL="https://github.com/bia-pain-bache/BPB-Worker-Panel/releases/latest/download/worker.js"
ARCHIVE_URL="https://github.com/bia-pain-bache/BPB-Wizard/releases/latest/download/${ARCHIVE}"

LATEST_VERSION=$(curl -fsSL https://raw.githubusercontent.com/bia-pain-bache/BPB-Wizard/main/VERSION)

mkdir -p "${INSTALL_DIR}"

if [ -x "${INSTALL_DIR}/${BINARY}" ]; then
    INSTALLED_VERSION=$("${INSTALL_DIR}/${BINARY}" --version)
    echo "Installed version: $INSTALLED_VERSION"
    echo "Latest version: ${LATEST_VERSION}"

    if [ "${INSTALLED_VERSION}" = "${LATEST_VERSION}" ]; then
        echo "Wizard is up to date."
    else
        echo "Updating to version ${LATEST_VERSION}..."
    NEEDS_INSTALL=1
    fi
else
    echo "Wizard not found here. Installing version ${LATEST_VERSION}..."
    NEEDS_INSTALL=1
fi

if [ "${NEEDS_INSTALL}" = "1" ]; then
    echo "Downloading ${ARCHIVE}..."
    curl -L -# -o "${ARCHIVE}" "${ARCHIVE_URL}"

    if [ "$EXT" = "zip" ]; then
        unzip -q -o "${ARCHIVE}" -d "${INSTALL_DIR}"
    else
        tar xzf "${ARCHIVE}" -C "${INSTALL_DIR}"
    fi

    rm -f "${ARCHIVE}"
    chmod +x "${INSTALL_DIR}/${BINARY}"
fi

echo "Downloading worker.js..."
curl -fsSL -o "${INSTALL_DIR}/worker.js" "${WORKER_URL}"

export LAUNCHED_BY_SCRIPT="1"

cd "${INSTALL_DIR}" || exit 1
exec "./${BINARY}"