#!/bin/bash
# Builds the Linux release: release/choboterm-linux-amd64.tar.gz.
#
# The binary is built in an Ubuntu 22.04 container (tools/linux/Dockerfile) so
# it runs on older distributions too; --native builds with this machine's Go
# and libraries instead. The frontend (frontend/dist) is built with npm when
# npm is available, otherwise an existing build (e.g. from build.bat) is used.
#
#   ./build_linux.sh [--native]      (from WSL: wsl -- ./build_linux.sh)
set -euo pipefail
cd "$(dirname "$0")"

version=$(sed -n 's/^const AppVersion = "\(.*\)"/\1/p' main.go)
name=choboterm-linux-amd64

# npm of Windows (seen from WSL as /mnt/c/...) can't build here.
if command -v npm >/dev/null 2>&1 && [[ "$(command -v npm)" != /mnt/* ]]; then
    echo "[1/4] frontend"
    (cd frontend && npm ci --silent && npm run build --silent)
elif [ -f frontend/dist/index.html ]; then
    echo "[1/4] frontend: using the existing frontend/dist"
else
    echo "frontend/dist is missing: install npm, or run build.bat first" >&2
    exit 1
fi

tags=desktop,production,webkit2_41
ldflags="-s -w"
if [ "${1:-}" = "--native" ]; then
    echo "[2/4] test (native)"
    go test ./...
    echo "[3/4] build (native)"
    CGO_ENABLED=1 go build -tags "$tags" -ldflags "$ldflags" -o "build/bin/choboterm" .
else
    echo "[2/4] build environment (docker image choboterm-linux-build)"
    docker build -q -t choboterm-linux-build tools/linux >/dev/null
    run() {
        docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod \
            -v "$PWD:/src" -v choboterm-go-cache:/cache -w /src choboterm-linux-build "$@"
    }
    docker volume create choboterm-go-cache >/dev/null
    docker run --rm -v choboterm-go-cache:/cache ubuntu:22.04 chown "$(id -u):$(id -g)" /cache
    echo "[3/4] test and build (Ubuntu 22.04)"
    run go test ./...
    run env CGO_ENABLED=1 go build -tags "$tags" -ldflags "$ldflags" -o build/bin/choboterm .
fi

echo "[4/4] package"
stage=$(mktemp -d)
mkdir "$stage/$name"
install -m 755 build/bin/choboterm "$stage/$name/choboterm"
install -m 644 build/appicon.png "$stage/$name/choboterm.png"
install -m 644 tools/linux/choboterm.desktop tools/linux/README.txt "$stage/$name/"
install -m 755 tools/linux/install.sh "$stage/$name/"
mkdir -p release
tar -C "$stage" -czf "release/$name.tar.gz" "$name"
rm -rf "$stage"
echo "done: release/$name.tar.gz (V$version)"
