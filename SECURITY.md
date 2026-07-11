# Security Policy

## Supported versions

Argonaut is in early development. Security fixes are made against the latest
release and the `main` branch; older tagged releases are not maintained.

## Reporting a vulnerability

**Please do not report security issues in public GitHub issues, pull requests, or
discussions.**

Report vulnerabilities privately through GitHub's built-in reporting:

1. Open the repository's **Security** tab.
2. Click **Report a vulnerability** (Security advisories → private vulnerability
   reporting).
3. Describe the issue with enough detail to reproduce it.

Helpful details to include:

- the affected version (`argonaut --version`) and platform,
- the Ceph release, and how Argonaut was built (`goceph` tag or mock),
- steps to reproduce, the impact, and any suggested fix.

As an early-stage project, responses are best-effort: we aim to acknowledge a
report within a few days and will coordinate a fix and disclosure with you.

## Scope notes

- Argonaut runs with the privileges of the operator and the Ceph keyring it is
  given, and it performs cluster-mutating operations — treat it like the `ceph`
  CLI. Protect the host and keyring accordingly.
- The `goceph` build dynamically links the Ceph client libraries
  (librados/librbd); vulnerabilities in those libraries should be reported
  upstream to the [Ceph project](https://ceph.io/).
