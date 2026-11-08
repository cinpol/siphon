// Package version knows about Ceph releases and Siphon's support matrix.
//
// It exists so that release identification and capability decisions live in
// one place instead of being scattered as magic numbers across the codebase.
// Siphon is tested against Squid (19), Tentacle (20) and Umbrella (21); Reef
// (18) is still recognised but no longer tested (see docs/testing.md for the
// support tiers). The version-aware decoding layer (internal/ceph/decode) uses
// this package to adapt when Ceph's JSON schemas differ between releases.
package version

import (
	"fmt"
	"regexp"
	"strconv"
)

// Build information for Siphon itself. These are vars (not consts) so a
// release build can stamp them via the linker, e.g.:
//
//	go build -ldflags "-X github.com/cinpol/siphon/internal/version.Version=1.2.3 \
//	    -X github.com/cinpol/siphon/internal/version.Commit=$(git rev-parse --short HEAD) \
//	    -X github.com/cinpol/siphon/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// The defaults apply to a plain `go build`/`go run` (a dev checkout).
var (
	// Version is Siphon's own version. The "-dev" suffix marks an
	// un-stamped build; release tooling overwrites it with the tag.
	Version = "0.7.0-dev"
	// Commit is the git revision the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC 3339, UTC).
	Date = "unknown"
)

// String is a one-line human-readable build identifier, e.g.
// "siphon 1.2.3 (commit abc1234, built 2026-07-08T10:00:00Z)".
func String() string {
	return fmt.Sprintf("siphon %s (commit %s, built %s)", Version, Commit, Date)
}

// Release is a named Ceph release with its numeric major version.
type Release struct {
	Name  string
	Major int
}

// The releases Siphon recognises, oldest to newest. Squid, Tentacle and
// Umbrella are tested; Reef is kept so Detect still names it canonically
// (best-effort, not tested).
var (
	Reef     = Release{Name: "reef", Major: 18}
	Squid    = Release{Name: "squid", Major: 19}
	Tentacle = Release{Name: "tentacle", Major: 20}
	Umbrella = Release{Name: "umbrella", Major: 21}
)

// byMajor maps a major version to its canonical Release, for Detect.
var byMajor = map[int]Release{
	Reef.Major:     Reef,
	Squid.Major:    Squid,
	Tentacle.Major: Tentacle,
	Umbrella.Major: Umbrella,
}

// versionRe matches strings like:
//
//	ceph version 20.1.0 (abc123...) tentacle (stable)
//	ceph version 21.2.0 (b060042...) umbrella (dev - RelWithDebInfo)
//
// capturing the major version and the release code name.
var versionRe = regexp.MustCompile(`ceph version (\d+)\.\d+\.\d+[^)]*\)\s+(\w+)`)

// Detect parses a Ceph version string into a Release. If the major version
// is one Siphon recognises, the canonical Release is returned; otherwise a
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
