# Testing against Ceph releases

This is a maintainer/contributor guide to how Argonaut is tested and how we
decide it "supports" a given Ceph release. If you just want to build and run the
project, start with [CONTRIBUTING.md](../CONTRIBUTING.md) and the README's
**Requirements** section instead.

Argonaut targets three Ceph releases at go-live — **Reef (18)**, **Squid (19)**
and **Tentacle (20)**. That list lives in one place, `internal/version`
(`version.Supported`), and the decoding layer (`internal/ceph/decode`) reads it
to adapt when Ceph's admin-command JSON differs between releases.

## Two separate questions

"Does Argonaut support Ceph release X?" is really two independent questions, and
they are tested in different ways:

1. **Build/link compatibility** — does the real (cgo + librados) binary
   *compile and link* against release X's client libraries? This is about the C
   ABI of `librados`/`librbd`, not about cluster behaviour. A binary linked
   against one client version generally talks fine to a cluster running a
   different (compatible) version, exactly like the `ceph` CLI does.

2. **Functional / behavioural compatibility** — does Argonaut *parse* release
   X's admin-command JSON and *drive* its operations correctly? Ceph
   occasionally changes command output shapes and command names between
   releases. This is where real support is won or lost, and it is caught by unit
   tests over `internal/ceph/decode` plus functional runs against a live
   cluster.

The two are deliberately decoupled: CI can prove the build links everywhere
cheaply and without a cluster, while behavioural coverage grows through fixtures
and occasional live runs.

## Dimension 1 — build/link compatibility

### Two jobs in CI

`.github/workflows/distros.yml` splits the build/link question in two, both
smoke-tested with `--version` (which links `librados` and exits — no cluster
needed):

- **`build`** — does it build on each supported **distro** (Ubuntu 22.04/24.04,
  Debian 12/13, AlmaLinux 9), using that distro's natural client source?
  Debian/Ubuntu install their own `librados-dev`/`librbd-dev`; the RHEL family
  ships no `librados-devel`, so those images pull it from the upstream Ceph repo
  pinned to the `CEPH_RELEASE` env default (currently `squid`).

- **`release-build`** — does it build against each supported Ceph **release**?
  A dedicated job whose matrix pins each release from the upstream repo on a
  matching AlmaLinux base. `el9` carries every supported release; `el10`
  currently only exists for tentacle, so that's the sole el10 cell:

  ```yaml
  release-build:
    container: ${{ matrix.image }}
    strategy:
      matrix:
        include:
          - { ceph: reef,     el: 9,  image: almalinux:9 }
          - { ceph: squid,    el: 9,  image: almalinux:9 }
          - { ceph: tentacle, el: 9,  image: almalinux:9 }
          - { ceph: tentacle, el: 10, image: almalinux:10 }
    # …installs librados-devel from
    #   https://download.ceph.com/rpm-${{ matrix.ceph }}/el${{ matrix.el }}/$basearch
    #   then: go build -tags goceph -buildvcs=false && argonaut --version
  ```

When adding a future release, add a cell and confirm the upstream repo publishes
`librados-devel` for that release on the chosen `el` — check
`download.ceph.com/rpm-<release>/el<n>/`; not every release builds for every EL
version (e.g. only tentacle publishes `el10` today).

### Building against a specific release locally

Same idea, by hand. On Debian/Ubuntu you get the distro's client:

```sh
sudo apt-get install -y librados-dev librbd-dev gcc pkg-config
go build -tags goceph ./cmd/argonaut
```

To pin a specific upstream release instead, add Ceph's own apt/rpm repo before
installing the `-dev` packages:

```sh
# Debian/Ubuntu, pin to a named release (e.g. squid)
CEPH_RELEASE=squid
. /etc/os-release                       # provides $VERSION_CODENAME
curl -fsSL https://download.ceph.com/keys/release.asc \
  | sudo gpg --dearmor -o /usr/share/keyrings/ceph.gpg
echo "deb [signed-by=/usr/share/keyrings/ceph.gpg] \
  https://download.ceph.com/debian-${CEPH_RELEASE}/ ${VERSION_CODENAME} main" \
  | sudo tee /etc/apt/sources.list.d/ceph.list
sudo apt-get update
sudo apt-get install -y librados-dev librbd-dev
```

```sh
# RHEL family, pin to a named release (mirrors distros.yml)
CEPH_RELEASE=squid; el=9
sudo tee /etc/yum.repos.d/ceph.repo >/dev/null <<EOF
[ceph]
name=Ceph \$basearch
baseurl=https://download.ceph.com/rpm-${CEPH_RELEASE}/el${el}/\$basearch
enabled=1
gpgcheck=1
gpgkey=https://download.ceph.com/keys/release.asc
EOF
sudo dnf -y install librados-devel librbd-devel
```

`argonaut --version` after building confirms the binary links.

## Dimension 2 — functional / behavioural compatibility

Linking is necessary but not sufficient. The parts that actually differ between
releases are:

- **Command output shapes** — the JSON returned by `ceph … -f json` gains,
  renames, or restructures fields over time.
- **Command names/arguments** — some admin commands are renamed or gain/lose
  arguments across majors.

There are two layers of defence here.

### Golden JSON fixtures (the primary mechanism)

The `internal/ceph/decode` package is the single choke point where raw Ceph JSON
becomes Argonaut's transport-agnostic `internal/model` types. Because it is pure
(bytes in, models out, no cluster), it is the cheapest place to lock down
cross-release behaviour: capture **real** admin-command output from each
supported release and assert it decodes correctly.

`decode_test.go` holds focused unit tests with inline JSON, while
`fixtures_test.go` (`TestFixtures`) runs **every decoder over every release's**
captured output under `testdata/`, organised by release:

```
internal/ceph/decode/testdata/
  reef/       health.json  osd_df_tree.json  pg_ls.json  df.json  ...
  squid/      health.json  osd_df_tree.json  pg_ls.json  df.json  ...
  tentacle/   health.json  osd_df_tree.json  pg_ls.json  df.json  ...
```

All three supported releases are covered today; a new one is added just by
dropping its `testdata/<release>/` directory in. When Ceph changes a schema in a
new release, the failing decode test tells us exactly what drifted — before a
user hits it.

**Capturing fixtures** from any reachable cluster (see the disposable-cluster
recipe below). This is the full set of admin commands Argonaut decodes — one
file each:

```sh
REL=squid   # or reef / tentacle — the release this cluster runs
DIR=internal/ceph/decode/testdata/$REL
mkdir -p "$DIR"

ceph health detail       -f json > "$DIR/health.json"
ceph status              -f json > "$DIR/status.json"
ceph version             -f json > "$DIR/version.json"
ceph df                  -f json > "$DIR/df.json"
ceph osd dump            -f json > "$DIR/osd_dump.json"
ceph osd df tree         -f json > "$DIR/osd_df_tree.json"
ceph osd crush tree      -f json > "$DIR/osd_crush_tree.json"
ceph osd crush rule dump -f json > "$DIR/osd_crush_rule_dump.json"
ceph pg ls               -f json > "$DIR/pg_ls.json"
ceph orch ls             -f json > "$DIR/orch_ls.json"   # cephadm-managed clusters
ceph orch ps             -f json > "$DIR/orch_ps.json"   # cephadm-managed clusters
```

`orch ls`/`orch ps` only return data on cephadm-managed clusters; on others
they're expected to be empty or error, which is itself worth knowing. Keep each
fixture small (a few OSDs/pools/PGs is enough to exercise the shape). The
fixtures in this repo come from a disposable, private test cluster, so their
internal IPs/FSIDs/hostnames are committed as-is; if you capture from a cluster
whose addressing is sensitive, sanitize it first — the tests assert structure,
not values, so placeholder values pass just the same.

### Live functional runs on a disposable cluster

Fixtures cover decoding; they don't cover the *write* side (that an operation
issues the right command and the cluster accepts it). For that, run Argonaut
against a throwaway cluster of the target release. Two low-cost options:

- **cephadm on a single VM** — closest to production; also gives you a real
  keyring and `ceph.conf` for end-to-end auth testing. Point Argonaut at it with
  `--client auto`. The dev server in `CLAUDE.md` is suitable for this.

  ```sh
  # on a disposable VM, pin the release you want to test
  curl -fsSL https://download.ceph.com/rpm-squid/el9/noarch/cephadm -o cephadm
  sudo ./cephadm bootstrap --mon-ip <VM_IP>
  # then, from the same host:
  sudo argonaut --client auto
  ```

- **`vstart.sh`** from a Ceph source checkout of the target branch — fastest for
  a throwaway dev cluster on one machine, no root, easy to tear down. Good for
  capturing fixtures and quick manual passes; less representative of a real
  deployment than cephadm.

Because a live cluster is heavyweight, these runs are **manual/periodic**, not
per-PR. The per-PR guarantee is: `distros.yml` proves the build links, and the
`decode` fixtures prove parsing — the two cheap, cluster-free signals.

## Adding a new supported release — checklist

1. Add the release to `internal/version` (`version.Supported` + `byMajor`).
2. Add the release to the `release-build` job's `ceph` matrix in `distros.yml`
   and confirm the upstream repo publishes `librados-devel` for it on the base
   image's `el` version.
3. Capture golden fixtures into `internal/ceph/decode/testdata/<release>/` and
   extend the decode table test; fix any schema drift the new fixtures reveal.
4. Do one manual functional pass against a disposable cluster of that release,
   exercising at least one write operation per view.
5. Update the README's **Ceph releases** table.
