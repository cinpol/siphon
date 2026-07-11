// Package ceph defines the single seam between Argonaut and a Ceph cluster.
//
// Every interaction with Ceph goes through the Client interface declared here.
// Only implementations that live under internal/ceph (the go-ceph transport and
// the in-memory mock) are permitted to import a concrete transport such as
// go-ceph; the rest of the application — business logic and UI — depends solely
// on this interface. That boundary is what keeps Argonaut:
//
//   - testable: the whole app runs against internal/ceph/mock with no cluster;
//   - transport-swappable: go-ceph can be augmented or replaced without touching
//     the UI or business logic;
//   - honest about failure: all cluster access returns errors the layers above
//     can surface to the operator.
//
// The interface is intentionally aligned with individual Ceph admin commands
// (status, version, df, osd dump). Higher-level aggregation (e.g. building the
// dashboard) belongs in the service layer, not here.
package ceph

import (
	"context"

	"github.com/cinpol/argonaut/internal/model"
)

// Client is the abstract handle to a Ceph cluster. Methods take a context so
// callers can bound each request; implementations must respect cancellation.
type Client interface {
	// Ping verifies basic connectivity to the cluster.
	Ping(ctx context.Context) error

	// Status returns identity, health and live IO/recovery figures (the
	// `status` command / pgmap).
	Status(ctx context.Context) (*model.Status, error)

	// HealthDetail returns cluster health with per-check detail messages (the
	// `health detail` command). Status carries the same checks at summary level;
	// this adds the per-item detail lines the dashboard expands.
	HealthDetail(ctx context.Context) (*model.Health, error)

	// Version returns the Ceph release the cluster is running.
	Version(ctx context.Context) (*model.ClusterVersion, error)

	// Capacity returns cluster-wide raw utilisation (the `df` command).
	Capacity(ctx context.Context) (*model.Capacity, error)

	// Flags returns the set cluster-wide OSD flags (from `osd dump`).
	Flags(ctx context.Context) ([]string, error)

	// OSDs returns the cluster's OSDs with placement/utilisation state (from
	// `osd df tree`).
	OSDs(ctx context.Context) ([]model.OSD, error)

	// Pools returns the cluster's storage pools with configuration and
	// utilisation (composed from `osd dump`, `df` and `osd crush rule dump`).
	Pools(ctx context.Context) ([]model.Pool, error)

	// CrushTree returns the CRUSH hierarchy as a flat list of nodes with child
	// ids (from `osd crush tree`).
	CrushTree(ctx context.Context) ([]model.CrushNode, error)

	// CrushRules returns the CRUSH placement rules (from `osd crush rule dump`).
	CrushRules(ctx context.Context) ([]model.CrushRule, error)

	// Services returns cephadm-managed services (from `orch ls`).
	Services(ctx context.Context) ([]model.Service, error)

	// Daemons returns the daemons of a service (from `orch ps --service_name`).
	Daemons(ctx context.Context, serviceName string) ([]model.Daemon, error)

	// PGsByPool returns the placement groups of a pool (from `pg ls-by-pool`).
	PGsByPool(ctx context.Context, pool string) ([]model.PG, error)

	// PGs returns every placement group in the cluster (from `pg ls`).
	PGs(ctx context.Context) ([]model.PG, error)

	// Admin executes a mutating admin (mon) command that needs no decoded
	// result, returning an error if it fails. The service layer owns the
	// catalogue of operations (command text + safety metadata) and builds the
	// command payloads; the transport just executes them. This keeps mutation
	// definitions in one place and the equivalent-command preview honest.
	Admin(ctx context.Context, command map[string]any) error

	// Close releases any resources held by the client (connections, handles).
	Close() error
}
