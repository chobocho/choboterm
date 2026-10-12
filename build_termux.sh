#!/usr/bin/env bash
# Build the Termux-only headless release: release/choboterm-termux-arm64
set -euo pipefail
cd "$(dirname "$0")"

for tool in go npm; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: $tool is required (install it with pkg in Termux)" >&2
        exit 1
    fi
done

echo "[1/2] Build Termux web frontend"
(
    cd frontend
    npm ci --silent
    VITE_TERMUX=1 npm run build --silent -- --outDir dist-termux
)

echo "[2/2] Build headless Android/ARM64 binary"
mkdir -p release
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build \
    -tags headless,termux -trimpath -ldflags "-s -w" \
    -o release/choboterm-termux-arm64 .

echo "done: release/choboterm-termux-arm64"
