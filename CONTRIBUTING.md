# Contributing to Siphon

Thanks for your interest in improving Siphon. This guide covers how to build,
test, and submit changes.

## Ground rules

- **All changes go through a pull request.** Direct pushes to `main` are not
  allowed; every change is reviewed and must pass CI before it is merged.
- Keep pull requests small and focused — one logical change each.
- By contributing, you agree that your contributions are licensed under the
  project's [Apache-2.0 License](./LICENSE).

## Development setup

Siphon is written in Go (see `go.mod` for the required version). The real,
cluster-capable build links the Ceph client libraries via cgo, so you need their
development headers — see the README's **Requirements** section for the full
list. In short, on Debian/Ubuntu:

```sh
sudo apt-get install -y librados-dev librbd-dev gcc pkg-config
```

You can also develop entirely against the in-memory mock, with no Ceph installed:

```sh
make build-mock                  # pure-Go binary, mock client only
./bin/siphon-mock --client mock
```

## Building and testing

```sh
make build     # real binary (needs the librados/librbd dev headers)
make test      # unit + end-to-end tests, run against the mock
make vet
make fmt       # gofmt
```

CI runs `gofmt`, `go vet`, `go test` and a native (`goceph`) compile-check on
every pull request. Please make sure these pass locally first.

For how Siphon is tested against specific Ceph releases — the build/link
distro matrix, golden JSON fixtures, and disposable-cluster functional runs —
see [docs/testing.md](./docs/testing.md).

## Submitting a pull request

1. Fork the repository (or create a topic branch, if you have write access).
2. Make your change on a branch.
3. Ensure `make test`, `make vet` and `make fmt` leave the tree clean.
4. Open a pull request against `main` with a clear description of the change and
   why it is needed.
5. Address review feedback; a maintainer merges once it is approved and CI is
   green.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(pools): add pool overview
fix(crush): handle empty buckets
docs: update installation steps
```

## Design principles

Siphon favours simplicity, safety, maintainability and a consistent operator
experience. In particular:

- Keep the layers separate: UI (`internal/ui`), business logic
  (`internal/service`), the Ceph transport (`internal/ceph`), and domain models
  (`internal/model`). The UI never talks to a Ceph client directly.
- Any potentially destructive operation must be confirmed and should preview the
  equivalent `ceph` command.
- Match the style and conventions of the surrounding code.
