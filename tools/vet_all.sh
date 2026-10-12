#!/usr/bin/env bash
# Type-checks (go vet) the Go sources and their tests for every platform and
# build the project has, from any one PC: files for another OS or build tag
# are otherwise not even compiled. Run it after moving or renaming Go code.
#
#   tools/vet_all.sh
set -uo pipefail
cd "$(dirname "$0")/.."

failed=0
check() { # GOOS GOARCH tags
    printf '%-8s %-6s %-18s ' "$1" "$2" "${3:-(default)}"
    if out=$(GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go vet -tags "$3" ./... 2>&1); then
        echo ok
    else
        echo FAILED
        echo "$out" | sed 's/^/    /'
        failed=1
    fi
}

check windows amd64 ""                 # the window (Wails)
check windows amd64 headless           # choboterm-headless.exe
check linux   amd64 ""                 # the window on Linux
check linux   amd64 headless           # choboterm-headless-linux-amd64
check darwin  arm64 headless           # the *_other.go files
if [ -d frontend/dist-termux ]; then   # embedded by the Termux build
    check android arm64 headless,termux
else
    echo "android  arm64  headless,termux    skipped: frontend/dist-termux is missing (build_termux.sh makes it)"
fi
exit $failed
