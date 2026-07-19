# Running Siphon in Docker

Siphon talks to Ceph through librados, so the image bundles the Ceph client
libraries. It's published on Docker Hub:

```
docker.io/cinpol/siphon
```

## Run it

Siphon authenticates like the `ceph` CLI: it needs a reachable cluster with a
`ceph.conf` and a keyring. Mount your `/etc/ceph` into the container and run it
interactively (a TUI needs `-it`):

```sh
docker run --rm -it -v /etc/ceph:/etc/ceph:ro docker.io/cinpol/siphon
```

This works against **any** cluster the host can reach — cephadm, manual, or
external. If `ceph -s` works on the host, Siphon connects.

The image is **multi-arch** (`linux/amd64` + `linux/arm64`); Docker pulls the
architecture matching your host, so it runs natively on both x86-64 and arm64
(including Apple Silicon Macs, without emulation).

Notes:

- The container needs network access to the cluster's MONs. Default bridge
  networking is usually fine; use `--network host` if your MONs are only
  reachable on the host's network.
- `--rm` removes the container on exit; drop it to keep it around.

## Which Ceph client is bundled

The image ships the **Tentacle** client by default, which is compatible with
Reef, Squid and Tentacle clusters. To match your release exactly, rebuild with a
different `CEPH_RELEASE`:

```sh
make docker CEPH_RELEASE=squid
# or:
docker build --build-arg CEPH_RELEASE=squid -t cinpol/siphon .
```

## Build it yourself

```sh
make docker            # builds cinpol/siphon:dev
```

See also [Running Siphon in Kubernetes](./kubernetes.md).
