# syntax=docker/dockerfile:1

# Build and package Siphon with the Ceph client libraries it needs at runtime.
#
# Siphon talks to the cluster through librados (cgo), so both the build and the
# runtime pull the Ceph client from download.ceph.com, pinned to a release via
# the CEPH_RELEASE build-arg. librados is compatible across nearby releases, so
# the default image works with Reef, Squid and Tentacle clusters; rebuild with
# `--build-arg CEPH_RELEASE=<release>` to match yours exactly.

ARG CEPH_RELEASE=tentacle
ARG GO_VERSION=1.26

# ---- builder: compile the native (cgo + librados) binary ----
FROM golang:${GO_VERSION}-bookworm AS builder
ARG CEPH_RELEASE
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg; \
    curl -fsSL https://download.ceph.com/keys/release.asc \
      | gpg --dearmor -o /usr/share/keyrings/ceph.gpg; \
    echo "deb [signed-by=/usr/share/keyrings/ceph.gpg] https://download.ceph.com/debian-${CEPH_RELEASE} bookworm main" \
      > /etc/apt/sources.list.d/ceph.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends gcc libc6-dev pkg-config librados-dev librbd-dev; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build metadata stamped into the binary. The release workflow passes the tag
# version, short commit and build date; a plain `docker build` gets the
# defaults. buildvcs is off because the build context has no .git (see
# .dockerignore), so these are passed in rather than read from git.
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=1 go build -tags goceph -buildvcs=false \
    -ldflags "-X github.com/cinpol/siphon/internal/version.Version=${VERSION} -X github.com/cinpol/siphon/internal/version.Commit=${COMMIT} -X github.com/cinpol/siphon/internal/version.Date=${DATE}" \
    -o /out/siphon ./cmd/siphon

# ---- runtime: slim image with just the Ceph client shared libraries ----
FROM debian:bookworm-slim AS runtime
ARG CEPH_RELEASE
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg; \
    curl -fsSL https://download.ceph.com/keys/release.asc \
      | gpg --dearmor -o /usr/share/keyrings/ceph.gpg; \
    echo "deb [signed-by=/usr/share/keyrings/ceph.gpg] https://download.ceph.com/debian-${CEPH_RELEASE} bookworm main" \
      > /etc/apt/sources.list.d/ceph.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends librados2 librbd1; \
    apt-get purge -y --auto-remove curl gnupg; \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/siphon /usr/local/bin/siphon

# Advertise a colour-capable terminal. `docker run -it` and `kubectl exec -it`
# default to a bare TERM=xterm with no truecolor hint, which makes the TUI
# downsample its palette; this restores the full colours a modern terminal shows.
ENV TERM=xterm-256color \
    COLORTERM=truecolor

# A TUI needs a terminal (`docker run -it`). In Kubernetes the manifest overrides
# the command to sleep and you `kubectl exec -it … -- siphon`.
ENTRYPOINT ["siphon"]
