# Build environment for the Linux release: Ubuntu 22.04, so the binary runs on
# glibc 2.35 and later (Ubuntu 22.04+, Debian 12+, Fedora 36+...).
FROM ubuntu:22.04
ARG GO_VERSION=1.26.3
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential pkg-config ca-certificates curl \
        libgtk-3-dev libwebkit2gtk-4.1-dev \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:$PATH GOFLAGS=-buildvcs=false
