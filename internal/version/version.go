// Package version knows about Ceph releases and Argonaut's support matrix.
//
// It exists so that release identification and capability decisions live in
// one place instead of being scattered as magic numbers across the codebase.
// Argonaut targets three releases at go-live — Reef (18), Squid (19) and
// Tentacle (20) — and the version-aware decoding layer (internal/ceph/decode)
// uses this package to adapt when Ceph's JSON schemas differ between them.
package version

import (
	"fmt"
	"regexp"
	"strconv"
)

// Build information for Argonaut itself. These are vars (not consts) so a
// release build can stamp them via the linker, e.g.:
//
//	go build -ldflags "-X github.com/cinpol/argonaut/internal/version.Version=1.2.3 \
//	    -X github.com/cinpol/argonaut/internal/version.Commit=$(git rev-parse --short HEAD) \
//	    -X github.com/cinpol/argonaut/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// The defaults apply to a plain `go build`/`go run` (a dev checkout).
var (
	// Version is Argonaut's own version. The "-dev" suffix marks an
	// un-stamped build; release tooling overwrites it with the tag.
	Version = "0.1.0-dev"
	// Commit is the git revision the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC 3339, UTC).
	Date = "unknown"
)

// String is a one-line human-readable build identifier, e.g.
// "argonaut 1.2.3 (commit abc1234, built 2026-07-08T10:00:00Z)".
func String() string {
	return fmt.Sprintf("argonaut %s (commit %s, built %s)", Version, Commit, Date)
}

// Release is a named Ceph release with its numeric major version.
type Release struct {
	Name  string
	Major int
}

// The releases Argonaut targets. Tentacle is the current primary; all three
// are supported at go-live.
var (
	Reef     = Release{Name: "reef", Major: 18}
	Squid    = Release{Name: "squid", Major: 19}
	Tentacle = Release{Name: "tentacle", Major: 20}
)

// Supported is Argonaut's support matrix, ordered oldest to newest.
var Supported = []Release{Reef, Squid, Tentacle}

var byMajor = map[int]Release{
	Reef.Major:     Reef,
	Squid.Major:    Squid,
	Tentacle.Major: Tentacle,
}

// FromMajor returns the named release for a major version, if known.
func FromMajor(major int) (Release, bool) {
	r, ok := byMajor[major]
	return r, ok
}

// versionRe matches strings like:
//
//	ceph version 20.1.0 (abc123...) tentacle (stable)
//
// capturing the major version and the release code name.
var versionRe = regexp.MustCompile(`ceph version (\d+)\.\d+\.\d+[^)]*\)\s+(\w+)`)

// Detect parses a Ceph version string into a Release. If the major version
// is one Argonaut recognises, the canonical Release is returned; otherwise a
// best-effort Release built from the parsed values is returned so callers can
// still surface *something* to the operator.
func Detect(raw string) (Release, bool) {
	m := versionRe.FindStringSubmatch(raw)
	if m == nil {
		return Release{}, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return Release{}, false
	}
	if r, ok := byMajor[major]; ok {
		return r, true
	}
	return Release{Name: m[2], Major: major}, true
}

// IsSupported reports whether the release is within Argonaut's support matrix.
func (r Release) IsSupported() bool {
	_, ok := byMajor[r.Major]
	return ok
}
