// Package model holds Siphon's domain types.
//
// These types are the vocabulary shared across every layer (API, business
// logic, UI). They are deliberately transport-agnostic: nothing here knows how
// the data was obtained (go-ceph, a mock, or anything else). Keeping the domain
// model separate from the transport is what lets the UI and business logic be
// developed and tested without a live Ceph cluster.
package model

import (
	"fmt"
	"strings"
)

// HealthStatus is Ceph's overall health assessment for the cluster.
type HealthStatus string

const (
	HealthOK      HealthStatus = "HEALTH_OK"
	HealthWarn    HealthStatus = "HEALTH_WARN"
	HealthErr     HealthStatus = "HEALTH_ERR"
	HealthUnknown HealthStatus = "HEALTH_UNKNOWN"
)

// HealthCheck is a single named check contributing to the cluster's health,
// e.g. OSD_NEARFULL. Ceph reports these as a map keyed by Code; we flatten them
// into a slice so the UI can render them in a stable order.
type HealthCheck struct {
	Code     string   // machine code, e.g. "OSD_NEARFULL"
	Severity string   // "HEALTH_WARN" | "HEALTH_ERR"
	Summary  string   // human-readable one-line summary
	Details  []string // per-item detail lines (from `ceph health detail`)
}

// Health is the cluster's overall status plus any active checks.
type Health struct {
	Status HealthStatus
	Checks []HealthCheck
}

// ClusterVersion identifies the Ceph release the cluster is running.
//
// Release/Major are parsed from Raw. They drive the version-aware decoding
// layer: Siphon targets a support matrix of Reef (18), Squid (19) and
// Tentacle (20), and some admin-command JSON schemas differ between them.
type ClusterVersion struct {
	Raw     string // full version string as reported by Ceph
	Release string // named release, e.g. "tentacle"
	Major   int    // numeric major, e.g. 20
}

// ClientIO is the instantaneous client throughput as reported by the pgmap.
type ClientIO struct {
	ReadBytesSec  int64
	WriteBytesSec int64
	ReadOpsSec    int64
	WriteOpsSec   int64
}

// Recovery summarises rebalance/recovery activity and PG cleanliness.
type Recovery struct {
	RecoveringBytesSec int64
	MisplacedRatio     float64 // 0..1
	DegradedRatio      float64 // 0..1
	TotalPGs           int
	CleanPGs           int // PGs in an active+clean state
}

// Active reports whether the cluster is currently doing recovery/rebalance
// work or has PGs that are not active+clean.
func (r Recovery) Active() bool {
	return r.RecoveringBytesSec > 0 ||
		r.MisplacedRatio > 0 ||
		r.DegradedRatio > 0 ||
		(r.TotalPGs > 0 && r.CleanPGs < r.TotalPGs)
}

// Capacity is the cluster's raw storage utilisation (from `ceph df`).
type Capacity struct {
	TotalBytes int64
	UsedBytes  int64
	AvailBytes int64
}

// UsedRatio returns used/total in the range 0..1 (0 when total is unknown).
func (c Capacity) UsedRatio() float64 {
	if c.TotalBytes <= 0 {
		return 0
	}
	return float64(c.UsedBytes) / float64(c.TotalBytes)
}

// Status is the snapshot returned by the `status` command: identity, health,
// and the live pgmap-derived IO and recovery figures.
type Status struct {
	FSID     string
	Health   Health
	IO       ClientIO
	Recovery Recovery
}

// OSD is a single object storage daemon and its placement/utilisation state.
//
// In/Up are distinct: Up is daemon liveness; In is whether the OSD participates
// in data placement. Following Ceph's own tooling, In is derived from the OSD
// (override) reweight — a reweight of 0 means the OSD is out.
type OSD struct {
	ID          int
	Host        string
	DeviceClass string
	Up          bool
	In          bool
	Reweight    float64 // OSD override weight, 0..1
	CrushWeight float64
	UsedRatio   float64 // 0..1
	PGs         int
	SizeBytes   int64
	UsedBytes   int64
}

// Status renders the up/down + in/out state as a compact label, e.g. "up/in".
func (o OSD) Status() string {
	up := "down"
	if o.Up {
		up = "up"
	}
	in := "out"
	if o.In {
		in = "in"
	}
	return up + "/" + in
}

// Pool is a Ceph storage pool with its configuration and utilisation.
type Pool struct {
	ID            int
	Name          string
	Type          string // "replicated" | "erasure"
	Size          int
	MinSize       int
	PGNum         int
	PGPNum        int
	CrushRule     string
	AutoscaleMode string // "on" | "off" | "warn"
	Applications  []string
	UsedRatio     float64 // 0..1
	StoredBytes   int64
	Objects       int64
}

// Replication renders the size/min_size pair, e.g. "3/2".
func (p Pool) Replication() string {
	return fmt.Sprintf("%d/%d", p.Size, p.MinSize)
}

// CrushNode is one node in the CRUSH hierarchy — a bucket (root, datacenter,
// room, row, rack, host, …) or an OSD leaf. Children holds child node ids; the
// UI builds the tree from these. TypeID is CRUSH's numeric type ordinal, used to
// reason about which moves are valid (a node may only move under a bucket of a
// higher type).
type CrushNode struct {
	ID          int
	Name        string
	Type        string
	TypeID      int
	DeviceClass string // for OSD leaves
	CrushWeight float64
	Children    []int
}

// IsOSD reports whether the node is an OSD leaf (rather than a bucket).
func (n CrushNode) IsOSD() bool { return n.Type == "osd" }

// CrushRule is a CRUSH placement rule, with its steps summarised for display.
type CrushRule struct {
	ID    int
	Name  string
	Type  string // "replicated" | "erasure"
	Steps []string
}

// Service is a cephadm-managed service (a group of daemons of one type), from
// `ceph orch ls`.
type Service struct {
	Name      string
	Type      string
	Running   int
	Size      int
	Placement string
}

// Health summarises whether all expected daemons are running.
func (s Service) Healthy() bool { return s.Size > 0 && s.Running >= s.Size }

// Daemon is a single running daemon instance, from `ceph orch ps`.
type Daemon struct {
	Name    string
	Type    string
	Host    string
	Status  string // e.g. "running", "stopped", "error"
	Version string
}

// PG is a placement group with its state and up/acting sets. The up set is the
// OSDs CRUSH wants; the acting set is the OSDs actually serving it — they differ
// during recovery/backfill.
type PG struct {
	ID            string
	State         string
	Up            []int
	UpPrimary     int
	Acting        []int
	ActingPrimary int
	Objects       int64
	Bytes         int64
	LastScrub     string
	LastDeepScrub string
}

// Healthy reports whether the PG is in a clean state.
func (p PG) Healthy() bool { return strings.Contains(p.State, "active+clean") }

// Dashboard is the aggregate the overview screen renders. The service layer
// composes it from several cluster calls so the UI has a single, coherent
// snapshot to display.
type Dashboard struct {
	FSID     string
	Version  ClusterVersion
	Health   Health
	Capacity Capacity
	IO       ClientIO
	Recovery Recovery
	Flags    []string

	// Pools carries per-pool utilisation (name, %used, stored bytes) for the
	// dashboard's capacity breakdown. Only usage fields are populated (from `df`);
	// full pool configuration lives in the Pools view. Empty when there are no
	// pools or the section failed to load (see Unavailable).
	Pools []Pool

	// Unavailable lists overview sections that failed to load this cycle (e.g.
	// "capacity", "flags"). The UI renders those as unavailable rather than
	// blanking the whole screen — one flaky sub-call shouldn't hide everything.
	Unavailable []string
}

// SectionOK reports whether a named dashboard section loaded successfully.
func (d Dashboard) SectionOK(name string) bool {
	for _, u := range d.Unavailable {
		if u == name {
			return false
		}
	}
	return true
}
