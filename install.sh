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

BINARY="Wizard"
ARCHIVE="Che-Wizard-${OS_LOWER}-${ARCH}.${EXT}"
LATEST_VERSION=$(curl -fsSL https://raw.githubusercontent.com/bia-pain-bache/Che-Wizard/main/VERSION)

if [ -x "./${BINARY}" ]; then
    INSTALLED_VERSION=$("./${BINARY}" --version)
    echo "Installed version: $INSTALLED_VERSION"
    echo "Latest version: ${LATEST_VERSION}"

    if [ "${INSTALLED_VERSION}" = "${LATEST_VERSION}" ]; then
        echo "Wizard is up to date. Running..."
        exec ./"${BINARY}"
    else
        echo "Updating to version ${LATEST_VERSION}..."
    fi
else
    echo "Wizard not found on device. Installing version ${LATEST_VERSION}..."
fi

echo "Downloading ${ARCHIVE}..."
curl -L -# -o "${ARCHIVE}" "https://github.com/bia-pain-bache/Che-Wizard/releases/latest/download/${ARCHIVE}"

if [ "$EXT" = "zip" ]; then
    unzip -q -o "${ARCHIVE}"
else
    tar xzf "${ARCHIVE}"
fi

rm -f "${ARCHIVE}"
chmod +x "./${BINARY}" && \
exec ./"${BINARY}"