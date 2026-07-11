package decode

import (
	"testing"

	"github.com/cinpol/argonaut/internal/model"
)

func TestHealth(t *testing.T) {
	raw := []byte(`{
		"status": "HEALTH_WARN",
		"checks": {
			"OSD_NEARFULL": {"severity": "HEALTH_WARN", "summary": {"message": "1 nearfull osd(s)"}, "detail": [{"message": "osd.4 is near full"}]},
			"MON_CLOCK_SKEW": {"severity": "HEALTH_WARN", "summary": {"message": "clock skew detected"}}
		}
	}`)

	h, err := Health(raw)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if h.Status != model.HealthWarn {
		t.Errorf("status = %q, want %q", h.Status, model.HealthWarn)
	}
	if len(h.Checks) != 2 {
		t.Fatalf("got %d checks, want 2", len(h.Checks))
	}
	// Checks must be sorted by code for deterministic rendering.
	if h.Checks[0].Code != "MON_CLOCK_SKEW" || h.Checks[1].Code != "OSD_NEARFULL" {
		t.Errorf("checks not sorted by code: %+v", h.Checks)
	}
	if h.Checks[1].Summary != "1 nearfull osd(s)" {
		t.Errorf("unexpected summary: %q", h.Checks[1].Summary)
	}
	if len(h.Checks[1].Details) != 1 || h.Checks[1].Details[0] != "osd.4 is near full" {
		t.Errorf("unexpected details: %+v", h.Checks[1].Details)
	}
	// A check without a detail array yields no details.
	if len(h.Checks[0].Details) != 0 {
		t.Errorf("expected no details for %s, got %+v", h.Checks[0].Code, h.Checks[0].Details)
	}
}

func TestVersion(t *testing.T) {
	raw := []byte(`{"version": "ceph version 20.1.0 (abc123def) tentacle (stable)"}`)

	v, err := Version(raw)
	if err != nil {
		t.Fatalf("Version returned error: %v", err)
	}
	if v.Major != 20 {
		t.Errorf("major = %d, want 20", v.Major)
	}
	if v.Release != "tentacle" {
		t.Errorf("release = %q, want tentacle", v.Release)
	}
	if v.Raw == "" {
		t.Error("raw version string not preserved")
	}
}

func TestStatus(t *testing.T) {
	raw := []byte(`{
		"fsid": "b3e2f1a4-0000-1111-2222-333344445555",
		"health": {"status": "HEALTH_WARN", "checks": {}},
		"pgmap": {
			"read_bytes_sec": 125829120,
			"write_bytes_sec": 47185920,
			"read_op_per_sec": 1200,
			"write_op_per_sec": 340,
			"recovering_bytes_per_sec": 10485760,
			"misplaced_ratio": 0.22,
			"degraded_ratio": 0.0,
			"num_pgs": 256,
			"pgs_by_state": [
				{"state_name": "active+clean", "count": 250},
				{"state_name": "active+remapped+backfilling", "count": 6}
			]
		}
	}`)

	st, err := Status(raw)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if st.FSID != "b3e2f1a4-0000-1111-2222-333344445555" {
		t.Errorf("unexpected fsid: %q", st.FSID)
	}
	if st.IO.ReadBytesSec != 125829120 || st.IO.WriteOpsSec != 340 {
		t.Errorf("unexpected IO: %+v", st.IO)
	}
	if st.Recovery.TotalPGs != 256 || st.Recovery.CleanPGs != 250 {
		t.Errorf("unexpected PG counts: total=%d clean=%d", st.Recovery.TotalPGs, st.Recovery.CleanPGs)
	}
	if !st.Recovery.Active() {
		t.Error("expected recovery to be active (backfilling + misplaced)")
	}
}

func TestCapacity(t *testing.T) {
	raw := []byte(`{"stats": {"total_bytes": 43980465111040, "total_used_bytes": 13194139533312, "total_avail_bytes": 30786325577728}}`)

	c, err := Capacity(raw)
	if err != nil {
		t.Fatalf("Capacity returned error: %v", err)
	}
	if c.TotalBytes != 43980465111040 {
		t.Errorf("unexpected total: %d", c.TotalBytes)
	}
	if r := c.UsedRatio(); r < 0.29 || r > 0.31 {
		t.Errorf("used ratio = %.4f, want ~0.30", r)
	}
}

func TestOSDs(t *testing.T) {
	raw := []byte(`{
		"nodes": [
			{"id": -1, "name": "default", "type": "root", "children": [-3, -5]},
			{"id": -3, "name": "ceph-01", "type": "host", "children": [1, 0]},
			{"id": -5, "name": "ceph-02", "type": "host", "children": [12]},
			{"id": 0, "name": "osd.0", "type": "osd", "device_class": "ssd", "crush_weight": 1.5, "reweight": 1.0, "kb": 1000000, "kb_used": 310000, "utilization": 31.0, "pgs": 118, "status": "up"},
			{"id": 1, "name": "osd.1", "type": "osd", "device_class": "ssd", "crush_weight": 1.5, "reweight": 1.0, "kb": 1000000, "kb_used": 290000, "utilization": 29.0, "pgs": 112, "status": "up"},
			{"id": 12, "name": "osd.12", "type": "osd", "device_class": "hdd", "crush_weight": 3.0, "reweight": 0.0, "kb": 2000000, "kb_used": 840000, "utilization": 42.0, "pgs": 0, "status": "down"}
		]
	}`)

	osds, err := OSDs(raw)
	if err != nil {
		t.Fatalf("OSDs returned error: %v", err)
	}
	if len(osds) != 3 {
		t.Fatalf("got %d OSDs, want 3", len(osds))
	}
	// Sorted by ID.
	if osds[0].ID != 0 || osds[1].ID != 1 || osds[2].ID != 12 {
		t.Fatalf("OSDs not sorted by ID: %+v", osds)
	}
	// Host derived from the parent bucket.
	if osds[0].Host != "ceph-01" || osds[2].Host != "ceph-02" {
		t.Errorf("unexpected host mapping: %q / %q", osds[0].Host, osds[2].Host)
	}
	// Up/in derivation.
	if !osds[0].Up || !osds[0].In {
		t.Errorf("osd.0 should be up/in: %+v", osds[0])
	}
	if osds[2].Up || osds[2].In { // down + reweight 0 => down/out
		t.Errorf("osd.12 should be down/out: %+v", osds[2])
	}
	if osds[2].Status() != "down/out" {
		t.Errorf("osd.12 status = %q, want down/out", osds[2].Status())
	}
	if r := osds[0].UsedRatio; r < 0.30 || r > 0.32 {
		t.Errorf("osd.0 used ratio = %.3f, want ~0.31", r)
	}
	if osds[0].SizeBytes != 1000000*1024 {
		t.Errorf("osd.0 size = %d", osds[0].SizeBytes)
	}
}

func TestPools(t *testing.T) {
	raw := []byte(`{"pools": [
		{"pool": 2, "pool_name": "rbd", "type": 1, "size": 3, "min_size": 2, "pg_num": 128, "pg_placement_num": 128, "crush_rule": 0, "pg_autoscale_mode": "on", "application_metadata": {"rbd": {}}},
		{"pool": 1, "pool_name": ".mgr", "type": 1, "size": 3, "min_size": 2, "pg_num": 1, "pg_placement_num": 1, "crush_rule": 0, "pg_autoscale_mode": "on", "application_metadata": {"mgr": {}}},
		{"pool": 5, "pool_name": "ec-data", "type": 3, "size": 6, "min_size": 4, "pg_num": 256, "pg_placement_num": 256, "crush_rule": 1, "pg_autoscale_mode": "warn", "application_metadata": {"rgw": {}}}
	]}`)

	pools, err := Pools(raw)
	if err != nil {
		t.Fatalf("Pools error: %v", err)
	}
	if len(pools) != 3 {
		t.Fatalf("got %d pools, want 3", len(pools))
	}
	// Sorted by ID.
	if pools[0].ID != 1 || pools[1].ID != 2 || pools[2].ID != 5 {
		t.Fatalf("pools not sorted by id: %+v", pools)
	}
	rbd := pools[1]
	if rbd.Name != "rbd" || rbd.Type != "replicated" || rbd.Replication() != "3/2" {
		t.Errorf("unexpected rbd pool: %+v", rbd)
	}
	if len(rbd.Applications) != 1 || rbd.Applications[0] != "rbd" {
		t.Errorf("unexpected applications: %v", rbd.Applications)
	}
	if pools[2].Type != "erasure" {
		t.Errorf("ec-data should be erasure, got %q", pools[2].Type)
	}
}

func TestPoolUsageAndRuleNames(t *testing.T) {
	usage, err := PoolUsage([]byte(`{"pools": [{"name": "rbd", "stats": {"stored": 1073741824, "objects": 256, "percent_used": 0.05}}]}`))
	if err != nil {
		t.Fatalf("PoolUsage error: %v", err)
	}
	if u, ok := usage["rbd"]; !ok || u.Objects != 256 || u.StoredBytes != 1073741824 {
		t.Errorf("unexpected usage: %+v", usage)
	}

	names, err := CrushRuleNames([]byte(`[{"rule_id": 0, "rule_name": "replicated_rule"}, {"rule_id": 1, "rule_name": "ec_rule"}]`))
	if err != nil {
		t.Fatalf("CrushRuleNames error: %v", err)
	}
	if names[0] != "replicated_rule" || names[1] != "ec_rule" {
		t.Errorf("unexpected rule names: %v", names)
	}
}

func TestPGs(t *testing.T) {
	t.Run("array form", func(t *testing.T) {
		raw := []byte(`[
			{"pgid": "2.0", "state": "active+clean", "up": [0,3,4], "up_primary": 0, "acting": [0,3,4], "acting_primary": 0, "stat_sum": {"num_objects": 412, "num_bytes": 1073741824}},
			{"pgid": "2.a", "state": "active+remapped+backfilling", "up": [2,3,4], "up_primary": 2, "acting": [2,3], "acting_primary": 2, "stat_sum": {"num_objects": 401}}
		]`)
		pgs, err := PGs(raw)
		if err != nil {
			t.Fatalf("PGs error: %v", err)
		}
		if len(pgs) != 2 {
			t.Fatalf("got %d PGs, want 2", len(pgs))
		}
		if pgs[0].ID != "2.0" || !pgs[0].Healthy() || pgs[0].Objects != 412 {
			t.Errorf("unexpected pg 2.0: %+v", pgs[0])
		}
		if pgs[1].Healthy() { // remapped+backfilling
			t.Error("2.a should not be healthy")
		}
		if len(pgs[1].Acting) != 2 {
			t.Errorf("expected 2.a acting set of 2, got %v", pgs[1].Acting)
		}
	})

	t.Run("wrapped form", func(t *testing.T) {
		raw := []byte(`{"pg_stats": [{"pgid": "1.0", "state": "active+clean", "up": [1,2,3], "acting": [1,2,3]}]}`)
		pgs, err := PGs(raw)
		if err != nil {
			t.Fatalf("PGs error: %v", err)
		}
		if len(pgs) != 1 || pgs[0].ID != "1.0" {
			t.Errorf("unexpected wrapped result: %+v", pgs)
		}
	})
}

func TestServices(t *testing.T) {
	raw := []byte(`[
		{"service_name": "mgr", "service_type": "mgr", "status": {"running": 2, "size": 2}, "placement": {"count": 2}},
		{"service_name": "mon", "service_type": "mon", "status": {"running": 2, "size": 3}, "placement": {"label": "mon"}},
		{"service_name": "rgw.default", "service_type": "rgw", "status": {"running": 2, "size": 2}, "placement": {"hosts": ["ceph-01", "ceph-02"]}}
	]`)

	svcs, err := Services(raw)
	if err != nil {
		t.Fatalf("Services error: %v", err)
	}
	if len(svcs) != 3 {
		t.Fatalf("got %d services, want 3", len(svcs))
	}
	// Sorted by name: mgr, mon, rgw.default.
	if svcs[1].Name != "mon" || svcs[1].Placement != "label:mon" {
		t.Errorf("unexpected mon service: %+v", svcs[1])
	}
	if svcs[1].Healthy() { // 2/3 running
		t.Error("mon (2/3) should not be healthy")
	}
	if svcs[2].Placement != "ceph-01,ceph-02" {
		t.Errorf("unexpected rgw placement: %q", svcs[2].Placement)
	}
}

func TestDaemons(t *testing.T) {
	raw := []byte(`[
		{"daemon_name": "mon.ceph-01", "daemon_type": "mon", "hostname": "ceph-01", "status_desc": "running", "version": "20.1.0"},
		{"daemon_type": "mon", "daemon_id": "ceph-02", "hostname": "ceph-02", "status_desc": "stopped"}
	]`)

	dmns, err := Daemons(raw)
	if err != nil {
		t.Fatalf("Daemons error: %v", err)
	}
	if len(dmns) != 2 {
		t.Fatalf("got %d daemons, want 2", len(dmns))
	}
	if dmns[0].Name != "mon.ceph-01" || dmns[0].Host != "ceph-01" {
		t.Errorf("unexpected daemon: %+v", dmns[0])
	}
	// Name synthesised from type+id when daemon_name is absent.
	if dmns[1].Name != "mon.ceph-02" || dmns[1].Status != "stopped" {
		t.Errorf("unexpected synthesised daemon: %+v", dmns[1])
	}
}

func TestCrushNodes(t *testing.T) {
	raw := []byte(`{"nodes": [
		{"id": -1, "name": "default", "type": "root", "type_id": 11, "children": [-3]},
		{"id": -3, "name": "rack1", "type": "rack", "type_id": 3, "children": [-4]},
		{"id": -4, "name": "ceph-01", "type": "host", "type_id": 1, "children": [0, 1]},
		{"id": 0, "name": "osd.0", "type": "osd", "type_id": 0, "device_class": "ssd", "crush_weight": 1.5},
		{"id": 1, "name": "osd.1", "type": "osd", "type_id": 0, "device_class": "ssd", "crush_weight": 1.5}
	]}`)

	nodes, err := CrushNodes(raw)
	if err != nil {
		t.Fatalf("CrushNodes error: %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("got %d nodes, want 5", len(nodes))
	}
	if nodes[0].Type != "root" || len(nodes[0].Children) != 1 || nodes[0].Children[0] != -3 {
		t.Errorf("unexpected root node: %+v", nodes[0])
	}
	if !nodes[3].IsOSD() || nodes[3].DeviceClass != "ssd" {
		t.Errorf("expected osd.0 leaf with ssd class: %+v", nodes[3])
	}
}

func TestCrushRules(t *testing.T) {
	raw := []byte(`[
		{"rule_id": 0, "rule_name": "replicated_rule", "type": 1, "steps": [
			{"op": "take", "item_name": "default"},
			{"op": "chooseleaf_firstn", "num": 0, "type": "host"},
			{"op": "emit"}
		]}
	]`)

	rules, err := CrushRules(raw)
	if err != nil {
		t.Fatalf("CrushRules error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	r := rules[0]
	if r.Name != "replicated_rule" || r.Type != "replicated" {
		t.Errorf("unexpected rule: %+v", r)
	}
	if len(r.Steps) != 3 || r.Steps[0] != "take default" || r.Steps[1] != "chooseleaf firstn 0 type host" {
		t.Errorf("unexpected steps: %v", r.Steps)
	}
}

func TestFlags(t *testing.T) {
	t.Run("array form", func(t *testing.T) {
		flags, err := Flags([]byte(`{"flags": "noout,sortbitwise", "flags_set": ["noout", "norebalance"]}`))
		if err != nil {
			t.Fatalf("Flags returned error: %v", err)
		}
		if len(flags) != 2 || flags[0] != "noout" || flags[1] != "norebalance" {
			t.Errorf("expected flags_set to win: %v", flags)
		}
	})

	t.Run("string fallback", func(t *testing.T) {
		flags, err := Flags([]byte(`{"flags": "sortbitwise,noout"}`))
		if err != nil {
			t.Fatalf("Flags returned error: %v", err)
		}
		// Sorted for deterministic display.
		if len(flags) != 2 || flags[0] != "noout" || flags[1] != "sortbitwise" {
			t.Errorf("unexpected flags: %v", flags)
		}
	})
}
