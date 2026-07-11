// Package decode isolates the parsing of Ceph's admin-command JSON output.
//
// Ceph does not guarantee JSON schema stability across releases, so all
// version-sensitive decoding lives here behind small, pure, testable functions.
// When a schema diverges across Reef/Squid/Tentacle, add an adapter in this
// package rather than leaking version logic into the transport
// (internal/ceph/goceph) or the UI. Keeping the functions pure (bytes in, model
// out) means they can be unit-tested with recorded fixtures and no live cluster.
package decode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cinpol/argonaut/internal/model"
	"github.com/cinpol/argonaut/internal/version"
)

// healthJSON mirrors the shape Ceph uses for health information, both as the
// top-level "health" command output and as the nested "health" object inside
// "status" output. Reusing it keeps the two decoders consistent.
type healthJSON struct {
	Status string `json:"status"`
	Checks map[string]struct {
		Severity string `json:"severity"`
		Summary  struct {
			Message string `json:"message"`
		} `json:"summary"`
		// Detail is populated by the `health detail` command; it is empty for
		// the summary-level `status`/`health` output.
		Detail []struct {
			Message string `json:"message"`
		} `json:"detail"`
	} `json:"checks"`
}

func (h healthJSON) toModel() model.Health {
	out := model.Health{Status: model.HealthStatus(h.Status)}
	for code, c := range h.Checks {
		var details []string
		for _, d := range c.Detail {
			details = append(details, d.Message)
		}
		out.Checks = append(out.Checks, model.HealthCheck{
			Code:     code,
			Severity: c.Severity,
			Summary:  c.Summary.Message,
			Details:  details,
		})
	}
	// Ceph returns checks as an unordered map; sort by code so the UI renders
	// them deterministically across refreshes.
	sort.Slice(out.Checks, func(i, j int) bool {
		return out.Checks[i].Code < out.Checks[j].Code
	})
	return out
}

// Health decodes the JSON returned by the "health" mon command.
func Health(raw []byte) (*model.Health, error) {
	var payload healthJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}
	h := payload.toModel()
	return &h, nil
}

// Version decodes the JSON returned by the "version" mon command and, where
// possible, resolves the named Ceph release.
func Version(raw []byte) (*model.ClusterVersion, error) {
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode version: %w", err)
	}
	cv := &model.ClusterVersion{Raw: payload.Version}
	if rel, ok := version.Detect(payload.Version); ok {
		cv.Release = rel.Name
		cv.Major = rel.Major
	}
	return cv, nil
}

// Status decodes the JSON returned by the "status" mon command into identity,
// health, and the live IO/recovery figures carried in the pgmap. The cluster
// version is not populated here (status does not carry it reliably); callers
// combine this with Version.
func Status(raw []byte) (*model.Status, error) {
	var payload struct {
		FSID   string     `json:"fsid"`
		Health healthJSON `json:"health"`
		PGMap  struct {
			ReadBytesSec       int64   `json:"read_bytes_sec"`
			WriteBytesSec      int64   `json:"write_bytes_sec"`
			ReadOpPerSec       int64   `json:"read_op_per_sec"`
			WriteOpPerSec      int64   `json:"write_op_per_sec"`
			RecoveringBytesSec int64   `json:"recovering_bytes_per_sec"`
			MisplacedRatio     float64 `json:"misplaced_ratio"`
			DegradedRatio      float64 `json:"degraded_ratio"`
			NumPGs             int     `json:"num_pgs"`
			PGsByState         []struct {
				StateName string `json:"state_name"`
				Count     int    `json:"count"`
			} `json:"pgs_by_state"`
		} `json:"pgmap"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}

	clean := 0
	for _, s := range payload.PGMap.PGsByState {
		if strings.Contains(s.StateName, "active+clean") {
			clean += s.Count
		}
	}

	return &model.Status{
		FSID:   payload.FSID,
		Health: payload.Health.toModel(),
		IO: model.ClientIO{
			ReadBytesSec:  payload.PGMap.ReadBytesSec,
			WriteBytesSec: payload.PGMap.WriteBytesSec,
			ReadOpsSec:    payload.PGMap.ReadOpPerSec,
			WriteOpsSec:   payload.PGMap.WriteOpPerSec,
		},
		Recovery: model.Recovery{
			RecoveringBytesSec: payload.PGMap.RecoveringBytesSec,
			MisplacedRatio:     payload.PGMap.MisplacedRatio,
			DegradedRatio:      payload.PGMap.DegradedRatio,
			TotalPGs:           payload.PGMap.NumPGs,
			CleanPGs:           clean,
		},
	}, nil
}

// Capacity decodes the JSON returned by the "df" mon command into cluster-wide
// raw utilisation.
func Capacity(raw []byte) (*model.Capacity, error) {
	var payload struct {
		Stats struct {
			TotalBytes     int64 `json:"total_bytes"`
			TotalUsedBytes int64 `json:"total_used_bytes"`
			TotalAvailytes int64 `json:"total_avail_bytes"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode df: %w", err)
	}
	return &model.Capacity{
		TotalBytes: payload.Stats.TotalBytes,
		UsedBytes:  payload.Stats.TotalUsedBytes,
		AvailBytes: payload.Stats.TotalAvailytes,
	}, nil
}

// OSDs decodes the JSON returned by the "osd df tree" mon command into a flat,
// sorted list of OSDs. That single command carries per-OSD utilisation, PG
// count, reweight and CRUSH weight, plus the tree structure from which we derive
// each OSD's host (its parent bucket of type "host").
func OSDs(raw []byte) ([]model.OSD, error) {
	var payload struct {
		Nodes []struct {
			ID          int     `json:"id"`
			Name        string  `json:"name"`
			Type        string  `json:"type"`
			DeviceClass string  `json:"device_class"`
			CrushWeight float64 `json:"crush_weight"`
			Reweight    float64 `json:"reweight"`
			KB          int64   `json:"kb"`
			KBUsed      int64   `json:"kb_used"`
			Utilization float64 `json:"utilization"`
			PGs         int     `json:"pgs"`
			Status      string  `json:"status"`
			Children    []int   `json:"children"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode osd df tree: %w", err)
	}

	// Map each OSD id to the name of the host bucket that contains it.
	hostByOSD := make(map[int]string)
	for _, n := range payload.Nodes {
		if n.Type == "host" {
			for _, child := range n.Children {
				hostByOSD[child] = n.Name
			}
		}
	}

	var osds []model.OSD
	for _, n := range payload.Nodes {
		if n.Type != "osd" {
			continue
		}
		osds = append(osds, model.OSD{
			ID:          n.ID,
			Host:        hostByOSD[n.ID],
			DeviceClass: n.DeviceClass,
			Up:          n.Status == "up",
			In:          n.Reweight > 0,
			Reweight:    n.Reweight,
			CrushWeight: n.CrushWeight,
			UsedRatio:   n.Utilization / 100,
			PGs:         n.PGs,
			SizeBytes:   n.KB * 1024,
			UsedBytes:   n.KBUsed * 1024,
		})
	}

	sort.Slice(osds, func(i, j int) bool { return osds[i].ID < osds[j].ID })
	return osds, nil
}

// pgEntry mirrors a single PG's stats in "pg ls" / "pg ls-by-pool" output.
type pgEntry struct {
	PGID          string `json:"pgid"`
	State         string `json:"state"`
	Up            []int  `json:"up"`
	UpPrimary     int    `json:"up_primary"`
	Acting        []int  `json:"acting"`
	ActingPrimary int    `json:"acting_primary"`
	StatSum       struct {
		NumObjects int64 `json:"num_objects"`
		NumBytes   int64 `json:"num_bytes"`
	} `json:"stat_sum"`
	LastScrub     string `json:"last_scrub_stamp"`
	LastDeepScrub string `json:"last_deep_scrub_stamp"`
}

// PGs decodes placement groups from "pg ls" / "pg ls-by-pool" output. The shape
// varies across releases — sometimes a bare array, sometimes wrapped in an
// object with a "pg_stats" field — so both are handled.
func PGs(raw []byte) ([]model.PG, error) {
	var entries []pgEntry

	var wrapped struct {
		PGStats []pgEntry `json:"pg_stats"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		entries = wrapped.PGStats
	} else if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode pg ls: %w", err)
	}

	pgs := make([]model.PG, 0, len(entries))
	for _, e := range entries {
		pgs = append(pgs, model.PG{
			ID:            e.PGID,
			State:         e.State,
			Up:            e.Up,
			UpPrimary:     e.UpPrimary,
			Acting:        e.Acting,
			ActingPrimary: e.ActingPrimary,
			Objects:       e.StatSum.NumObjects,
			Bytes:         e.StatSum.NumBytes,
			LastScrub:     e.LastScrub,
			LastDeepScrub: e.LastDeepScrub,
		})
	}
	return pgs, nil
}

// Services decodes cephadm services from "orch ls" output.
func Services(raw []byte) ([]model.Service, error) {
	var payload []struct {
		Name   string `json:"service_name"`
		Type   string `json:"service_type"`
		Status struct {
			Running int `json:"running"`
			Size    int `json:"size"`
		} `json:"status"`
		Placement json.RawMessage `json:"placement"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode orch ls: %w", err)
	}

	services := make([]model.Service, 0, len(payload))
	for _, s := range payload {
		services = append(services, model.Service{
			Name:      s.Name,
			Type:      s.Type,
			Running:   s.Status.Running,
			Size:      s.Status.Size,
			Placement: formatPlacement(s.Placement),
		})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}

// formatPlacement renders a cephadm placement spec into a compact label. The
// spec varies in shape across releases, so it is parsed leniently.
func formatPlacement(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "*"
	}
	var p struct {
		Label       string            `json:"label"`
		Count       int               `json:"count"`
		HostPattern string            `json:"host_pattern"`
		Hosts       []json.RawMessage `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "*"
	}
	switch {
	case p.Label != "":
		return "label:" + p.Label
	case len(p.Hosts) > 0:
		return strings.Join(hostNames(p.Hosts), ",")
	case p.HostPattern != "":
		return p.HostPattern
	case p.Count > 0:
		return "count:" + strconv.Itoa(p.Count)
	default:
		return "*"
	}
}

// hostNames extracts hostnames from placement hosts, which may be plain strings
// or objects with a "hostname" field.
func hostNames(raw []json.RawMessage) []string {
	var out []string
	for _, h := range raw {
		var s string
		if err := json.Unmarshal(h, &s); err == nil {
			out = append(out, s)
			continue
		}
		var obj struct {
			Hostname string `json:"hostname"`
		}
		if err := json.Unmarshal(h, &obj); err == nil && obj.Hostname != "" {
			out = append(out, obj.Hostname)
		}
	}
	return out
}

// Daemons decodes daemon instances from "orch ps" output.
func Daemons(raw []byte) ([]model.Daemon, error) {
	var payload []struct {
		Name       string `json:"daemon_name"`
		Type       string `json:"daemon_type"`
		ID         string `json:"daemon_id"`
		Host       string `json:"hostname"`
		StatusDesc string `json:"status_desc"`
		Version    string `json:"version"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode orch ps: %w", err)
	}

	daemons := make([]model.Daemon, 0, len(payload))
	for _, d := range payload {
		name := d.Name
		if name == "" {
			name = d.Type + "." + d.ID
		}
		daemons = append(daemons, model.Daemon{
			Name:    name,
			Type:    d.Type,
			Host:    d.Host,
			Status:  d.StatusDesc,
			Version: d.Version,
		})
	}
	sort.Slice(daemons, func(i, j int) bool { return daemons[i].Name < daemons[j].Name })
	return daemons, nil
}

// CrushNodes decodes the CRUSH hierarchy from "osd crush tree" output into a
// flat list of nodes (each carrying its child ids). The UI builds the tree and
// computes bucket weights from the leaves.
func CrushNodes(raw []byte) ([]model.CrushNode, error) {
	var payload struct {
		Nodes []struct {
			ID          int     `json:"id"`
			Name        string  `json:"name"`
			Type        string  `json:"type"`
			TypeID      int     `json:"type_id"`
			DeviceClass string  `json:"device_class"`
			CrushWeight float64 `json:"crush_weight"`
			Children    []int   `json:"children"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode osd crush tree: %w", err)
	}

	nodes := make([]model.CrushNode, 0, len(payload.Nodes))
	for _, n := range payload.Nodes {
		nodes = append(nodes, model.CrushNode{
			ID:          n.ID,
			Name:        n.Name,
			Type:        n.Type,
			TypeID:      n.TypeID,
			DeviceClass: n.DeviceClass,
			CrushWeight: n.CrushWeight,
			Children:    n.Children,
		})
	}
	return nodes, nil
}

// CrushRules decodes CRUSH placement rules from "osd crush rule dump", with each
// rule's steps summarised into readable lines.
func CrushRules(raw []byte) ([]model.CrushRule, error) {
	var rules []struct {
		ID    int              `json:"rule_id"`
		Name  string           `json:"rule_name"`
		Type  int              `json:"type"`
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("decode crush rule dump: %w", err)
	}

	out := make([]model.CrushRule, 0, len(rules))
	for _, r := range rules {
		cr := model.CrushRule{ID: r.ID, Name: r.Name, Type: poolTypeName(r.Type)}
		for _, step := range r.Steps {
			cr.Steps = append(cr.Steps, formatCrushStep(step))
		}
		out = append(out, cr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// formatCrushStep renders a single CRUSH rule step map into a readable line,
// e.g. {"op":"chooseleaf_firstn","num":0,"type":"host"} -> "chooseleaf firstn 0 type host".
func formatCrushStep(step map[string]any) string {
	op, _ := step["op"].(string)
	parts := []string{strings.ReplaceAll(op, "_", " ")}
	if v, ok := step["item_name"]; ok {
		parts = append(parts, fmt.Sprint(v))
	}
	if v, ok := step["num"]; ok {
		parts = append(parts, numToString(v))
	}
	if v, ok := step["type"]; ok {
		parts = append(parts, "type", fmt.Sprint(v))
	}
	return strings.Join(parts, " ")
}

// numToString renders a JSON number (decoded as float64) without a trailing .0.
func numToString(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.Itoa(int(f))
	}
	return fmt.Sprint(v)
}

// poolTypeName maps Ceph's numeric pool type to a readable name.
func poolTypeName(t int) string {
	switch t {
	case 3:
		return "erasure"
	default:
		return "replicated"
	}
}

// Pools decodes the pool configuration from "osd dump" output. Utilisation and
// CRUSH-rule names are not in this payload; callers enrich the result with
// PoolUsage and CrushRuleNames.
func Pools(raw []byte) ([]model.Pool, error) {
	var payload struct {
		Pools []struct {
			ID          int                        `json:"pool"`
			Name        string                     `json:"pool_name"`
			Type        int                        `json:"type"`
			Size        int                        `json:"size"`
			MinSize     int                        `json:"min_size"`
			PGNum       int                        `json:"pg_num"`
			PGPNum      int                        `json:"pg_placement_num"`
			CrushRule   int                        `json:"crush_rule"`
			Autoscale   string                     `json:"pg_autoscale_mode"`
			Application map[string]json.RawMessage `json:"application_metadata"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode osd dump pools: %w", err)
	}

	pools := make([]model.Pool, 0, len(payload.Pools))
	for _, p := range payload.Pools {
		apps := make([]string, 0, len(p.Application))
		for name := range p.Application {
			apps = append(apps, name)
		}
		sort.Strings(apps)

		pools = append(pools, model.Pool{
			ID:            p.ID,
			Name:          p.Name,
			Type:          poolTypeName(p.Type),
			Size:          p.Size,
			MinSize:       p.MinSize,
			PGNum:         p.PGNum,
			PGPNum:        p.PGPNum,
			CrushRule:     strconv.Itoa(p.CrushRule), // replaced by name via CrushRuleNames
			AutoscaleMode: p.Autoscale,
			Applications:  apps,
		})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].ID < pools[j].ID })
	return pools, nil
}

// PoolUsage decodes per-pool utilisation from "df" output, keyed by pool name.
func PoolUsage(raw []byte) (map[string]model.Pool, error) {
	var payload struct {
		Pools []struct {
			Name  string `json:"name"`
			Stats struct {
				Stored      int64   `json:"stored"`
				Objects     int64   `json:"objects"`
				PercentUsed float64 `json:"percent_used"`
			} `json:"stats"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode df pools: %w", err)
	}

	usage := make(map[string]model.Pool, len(payload.Pools))
	for _, p := range payload.Pools {
		usage[p.Name] = model.Pool{
			UsedRatio:   p.Stats.PercentUsed,
			StoredBytes: p.Stats.Stored,
			Objects:     p.Stats.Objects,
		}
	}
	return usage, nil
}

// CrushRuleNames decodes an id->name map from "osd crush rule dump" output.
func CrushRuleNames(raw []byte) (map[int]string, error) {
	var rules []struct {
		ID   int    `json:"rule_id"`
		Name string `json:"rule_name"`
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("decode crush rule dump: %w", err)
	}
	names := make(map[int]string, len(rules))
	for _, r := range rules {
		names[r.ID] = r.Name
	}
	return names, nil
}

// Flags decodes the set cluster flags from "osd dump" output. Newer releases
// expose a "flags_set" array; older ones only a comma-separated "flags" string.
// We prefer the array and fall back to splitting the string.
func Flags(raw []byte) ([]string, error) {
	var payload struct {
		Flags    string   `json:"flags"`
		FlagsSet []string `json:"flags_set"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode osd dump flags: %w", err)
	}

	var flags []string
	if len(payload.FlagsSet) > 0 {
		flags = append(flags, payload.FlagsSet...)
	} else if payload.Flags != "" {
		for _, f := range strings.Split(payload.Flags, ",") {
			if f = strings.TrimSpace(f); f != "" {
				flags = append(flags, f)
			}
		}
	}
	sort.Strings(flags)
	return flags, nil
}
