//go:build !goceph

// This stub is compiled by default. The native go-ceph client requires cgo
// and librados development headers, which are not present on every build host
// (e.g. a developer laptop without Ceph installed). Gating the real client
// behind the `goceph` build tag lets Siphon compile and run everywhere using
// the mock, while production builds on a Ceph admin/MON node are produced with:
//
//	go build -tags goceph ./cmd/siphon
//
// See client_goceph.go for the real implementation.
package goceph

import (
	"errors"

	"github.com/cinpol/siphon/internal/ceph"
)

// New reports that this binary was built without go-ceph support. Callers
// (cmd/siphon) treat this error as "native client unavailable" and can fall
// back to the mock client.
func New(cfg Config) (ceph.Client, error) {
	return nil, errors.New("built without go-ceph support: rebuild with `-tags goceph` on a host that has librados development headers")
}
