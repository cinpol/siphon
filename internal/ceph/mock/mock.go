// Package mock provides an in-memory implementation of ceph.Client.
//
// It exists so the entire application can be developed, run and tested without
// a real Ceph cluster — which is essential while no cluster is available, and
// invaluable for fast, deterministic unit tests. The data is fabricated but
// shaped like real Ceph output (a cluster in HEALTH_WARN, ~30% full, with light
// client IO and a little backfill in progress) so the UI can be exercised
// against realistic states.
package mock

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cinpol/argonaut/internal/model"
)

// Client is a canned, in-memory ceph.Client. Admin commands mutate the in-memory
// OSD state so the UI reflects operations during development and tests.
type Client struct {
	fsid     string
	version  model.ClusterVersion
	health   model.Health
	io       model.ClientIO
	rec      model.Recovery
	cap      model.Capacity
	flags    []string
	osds     []model.OSD
	pools    []model.Pool
	crush    []model.CrushNode
	rules    []model.CrushRule
	services []model.Service
	daemons  map[string][]model.Daemon
	pgs      map[string][]model.PG

	// LastCommand records the most recent Admin command for assertions in tests.
	LastCommand map[string]any

	// Injectable errors for testing robustness/degradation. When set, the
	// matching read method returns the error instead of data.
	ErrStatus, ErrVersion, ErrCapacity, ErrFlags, ErrOSDs, ErrPools error
	// ErrAdmin, when set, makes every mutating Admin command fail with it (used
	// to exercise operation-failure paths such as the permission-hint handling).
	ErrAdmin error
}

// New returns a mock client preloaded with a demo cluster.
func New() *Client {
	return &Client{
		fsid: "b3e2f1a4-0000-1111-2222-333344445555",
		version: model.ClusterVersion{
			Raw:     "ceph version 20.1.0 (0000000000000000000000000000000000000000) tentacle (stable)",
			Release: "tentacle",
			Major:   20,
		},
		health: model.Health{
			Status: model.HealthWarn,
			Checks: []model.HealthCheck{
				{
					Code:     "OSD_NEARFULL",
					Severity: "HEALTH_WARN",
					Summary:  "1 nearfull osd(s)",
					Details:  []string{"osd.4 is near full"},
				},
				{
					Code:     "OSDMAP_FLAGS",
					Severity: "HEALTH_WARN",
					Summary:  "noout,norebalance flag(s) set",
					Details:  []string{"noout,norebalance flag(s) set"},
				},
			},
		},
		io: model.ClientIO{
			ReadBytesSec:  125829120, // 120 MiB/s
			WriteBytesSec: 47185920,  // 45 MiB/s
			ReadOpsSec:    1200,
			WriteOpsSec:   340,
		},
		rec: model.Recovery{
			RecoveringBytesSec: 10485760, // 10 MiB/s
			MisplacedRatio:     0.022,
			DegradedRatio:      0,
			TotalPGs:           256,
			CleanPGs:           250,
		},
		cap: model.Capacity{
			TotalBytes: 43980465111040, // 40 TiB
			UsedBytes:  13194139533312, // ~12 TiB
			AvailBytes: 30786325577728,
		},
		flags: []string{"noout", "norebalance"},
		osds: []model.OSD{
			{ID: 0, Host: "ceph-01", DeviceClass: "ssd", Up: true, In: true, Reweight: 1.0, CrushWeight: 1.5, UsedRatio: 0.31, PGs: 118, SizeBytes: 1_099_511_627_776, UsedBytes: 340_824_604_672},
			{ID: 1, Host: "ceph-01", DeviceClass: "ssd", Up: true, In: true, Reweight: 1.0, CrushWeight: 1.5, UsedRatio: 0.29, PGs: 112, SizeBytes: 1_099_511_627_776, UsedBytes: 318_923_866_112},
			{ID: 2, Host: "ceph-02", DeviceClass: "ssd", Up: true, In: true, Reweight: 1.0, CrushWeight: 1.5, UsedRatio: 0.33, PGs: 121, SizeBytes: 1_099_511_627_776, UsedBytes: 362_924_113_920},
			{ID: 3, Host: "ceph-02", DeviceClass: "hdd", Up: true, In: true, Reweight: 0.8, CrushWeight: 3.0, UsedRatio: 0.44, PGs: 96, SizeBytes: 3_298_534_883_328, UsedBytes: 1_451_355_348_664},
			{ID: 4, Host: "ceph-03", DeviceClass: "hdd", Up: false, In: false, Reweight: 0.0, CrushWeight: 3.0, UsedRatio: 0.42, PGs: 0, SizeBytes: 3_298_534_883_328, UsedBytes: 1_385_384_650_997},
		},
		pools: []model.Pool{
			{ID: 1, Name: ".mgr", Type: "replicated", Size: 3, MinSize: 2, PGNum: 1, PGPNum: 1, CrushRule: "replicated_rule", AutoscaleMode: "on", Applications: []string{"mgr"}, UsedRatio: 0.001, StoredBytes: 33_554_432, Objects: 12},
			{ID: 2, Name: "rbd", Type: "replicated", Size: 3, MinSize: 2, PGNum: 128, PGPNum: 128, CrushRule: "replicated_rule", AutoscaleMode: "on", Applications: []string{"rbd"}, UsedRatio: 0.05, StoredBytes: 214_748_364_800, Objects: 52_400},
			{ID: 3, Name: "cephfs_data", Type: "replicated", Size: 3, MinSize: 2, PGNum: 256, PGPNum: 256, CrushRule: "replicated_rule", AutoscaleMode: "warn", Applications: []string{"cephfs"}, UsedRatio: 0.18, StoredBytes: 805_306_368_000, Objects: 196_608},
			{ID: 4, Name: "ec-rgw-data", Type: "erasure", Size: 6, MinSize: 4, PGNum: 512, PGPNum: 512, CrushRule: "ec_rule", AutoscaleMode: "on", Applications: []string{"rgw"}, UsedRatio: 0.27, StoredBytes: 1_649_267_441_664, Objects: 402_653},
		},
		crush: []model.CrushNode{
			{ID: -1, Name: "default", Type: "root", TypeID: 11, Children: []int{-2, -3}},
			{ID: -2, Name: "rack1", Type: "rack", TypeID: 3, Children: []int{-4, -5}},
			{ID: -3, Name: "rack2", Type: "rack", TypeID: 3, Children: []int{-6}},
			{ID: -4, Name: "ceph-01", Type: "host", TypeID: 1, Children: []int{0, 1}},
			{ID: -5, Name: "ceph-02", Type: "host", TypeID: 1, Children: []int{2, 3}},
			{ID: -6, Name: "ceph-03", Type: "host", TypeID: 1, Children: []int{4}},
			{ID: 0, Name: "osd.0", Type: "osd", TypeID: 0, DeviceClass: "ssd", CrushWeight: 1.5},
			{ID: 1, Name: "osd.1", Type: "osd", TypeID: 0, DeviceClass: "ssd", CrushWeight: 1.5},
			{ID: 2, Name: "osd.2", Type: "osd", TypeID: 0, DeviceClass: "ssd", CrushWeight: 1.5},
			{ID: 3, Name: "osd.3", Type: "osd", TypeID: 0, DeviceClass: "hdd", CrushWeight: 3.0},
			{ID: 4, Name: "osd.4", Type: "osd", TypeID: 0, DeviceClass: "hdd", CrushWeight: 3.0},
		},
		rules: []model.CrushRule{
			{ID: 0, Name: "replicated_rule", Type: "replicated", Steps: []string{"take default", "chooseleaf firstn 0 type host", "emit"}},
			{ID: 1, Name: "ec_rule", Type: "erasure", Steps: []string{"set_chooseleaf_tries 5", "take default", "chooseleaf indep 0 type host", "emit"}},
		},
		services: []model.Service{
			{Name: "mon", Type: "mon", Running: 3, Size: 3, Placement: "label:mon"},
			{Name: "mgr", Type: "mgr", Running: 2, Size: 2, Placement: "count:2"},
			{Name: "osd", Type: "osd", Running: 5, Size: 5, Placement: "*"},
			{Name: "mds.cephfs", Type: "mds", Running: 2, Size: 2, Placement: "count:2"},
			{Name: "rgw.default", Type: "rgw", Running: 1, Size: 2, Placement: "ceph-01,ceph-02"},
		},
		daemons: map[string][]model.Daemon{
			"mon": {
				{Name: "mon.ceph-01", Type: "mon", Host: "ceph-01", Status: "running", Version: "20.1.0"},
				{Name: "mon.ceph-02", Type: "mon", Host: "ceph-02", Status: "running", Version: "20.1.0"},
				{Name: "mon.ceph-03", Type: "mon", Host: "ceph-03", Status: "running", Version: "20.1.0"},
			},
			"mgr": {
				{Name: "mgr.ceph-01.aab", Type: "mgr", Host: "ceph-01", Status: "running", Version: "20.1.0"},
				{Name: "mgr.ceph-02.ccd", Type: "mgr", Host: "ceph-02", Status: "running", Version: "20.1.0"},
			},
			"mds.cephfs": {
				{Name: "mds.cephfs.ceph-01.xyz", Type: "mds", Host: "ceph-01", Status: "running", Version: "20.1.0"},
				{Name: "mds.cephfs.ceph-02.uvw", Type: "mds", Host: "ceph-02", Status: "running", Version: "20.1.0"},
			},
			"rgw.default": {
				{Name: "rgw.default.ceph-01.rst", Type: "rgw", Host: "ceph-01", Status: "running", Version: "20.1.0"},
				{Name: "rgw.default.ceph-02.opq", Type: "rgw", Host: "ceph-02", Status: "stopped", Version: "20.1.0"},
			},
		},
		pgs: map[string][]model.PG{
			"rbd": {
				{ID: "2.0", State: "active+clean", Up: []int{0, 3, 4}, UpPrimary: 0, Acting: []int{0, 3, 4}, ActingPrimary: 0, Objects: 412, Bytes: 1_073_741_824, LastScrub: "2026-07-04T02:11:00", LastDeepScrub: "2026-06-30T01:03:00"},
				{ID: "2.1", State: "active+clean", Up: []int{1, 2, 0}, UpPrimary: 1, Acting: []int{1, 2, 0}, ActingPrimary: 1, Objects: 388, Bytes: 1_020_000_000, LastScrub: "2026-07-04T03:20:00", LastDeepScrub: "2026-06-29T22:41:00"},
				{ID: "2.a", State: "active+remapped+backfilling", Up: []int{2, 3, 4}, UpPrimary: 2, Acting: []int{2, 3}, ActingPrimary: 2, Objects: 401, Bytes: 1_050_000_000, LastScrub: "2026-07-03T18:02:00", LastDeepScrub: "2026-06-28T10:15:00"},
				{ID: "2.b", State: "active+clean+scrubbing", Up: []int{0, 1, 3}, UpPrimary: 0, Acting: []int{0, 1, 3}, ActingPrimary: 0, Objects: 377, Bytes: 990_000_000, LastScrub: "2026-07-05T06:00:00", LastDeepScrub: "2026-06-30T12:00:00"},
				{ID: "2.14", State: "active+undersized+degraded", Up: []int{1, 2}, UpPrimary: 1, Acting: []int{1, 2}, ActingPrimary: 1, Objects: 366, Bytes: 900_000_000, LastScrub: "2026-07-02T11:31:00", LastDeepScrub: "2026-06-27T09:44:00"},
			},
			"cephfs_data": {
				{ID: "3.0", State: "active+clean", Up: []int{2, 4, 0}, UpPrimary: 2, Acting: []int{2, 4, 0}, ActingPrimary: 2, Objects: 1500, Bytes: 6_442_450_944, LastScrub: "2026-07-04T04:00:00", LastDeepScrub: "2026-06-29T04:00:00"},
				{ID: "3.1", State: "active+clean", Up: []int{3, 0, 1}, UpPrimary: 3, Acting: []int{3, 0, 1}, ActingPrimary: 3, Objects: 1466, Bytes: 6_300_000_000, LastScrub: "2026-07-04T05:10:00", LastDeepScrub: "2026-06-29T05:10:00"},
			},
		},
	}
}

func (c *Client) Ping(ctx context.Context) error { return nil }

func (c *Client) Status(ctx context.Context) (*model.Status, error) {
	if c.ErrStatus != nil {
		return nil, c.ErrStatus
	}
	return &model.Status{
		FSID:     c.fsid,
		Health:   c.cloneHealth(),
		IO:       c.io,
		Recovery: c.rec,
	}, nil
}

func (c *Client) HealthDetail(ctx context.Context) (*model.Health, error) {
	if c.ErrStatus != nil {
		return nil, c.ErrStatus
	}
	h := c.cloneHealth()
	return &h, nil
}

func (c *Client) Version(ctx context.Context) (*model.ClusterVersion, error) {
	if c.ErrVersion != nil {
		return nil, c.ErrVersion
	}
	v := c.version
	return &v, nil
}

func (c *Client) Capacity(ctx context.Context) (*model.Capacity, error) {
	if c.ErrCapacity != nil {
		return nil, c.ErrCapacity
	}
	cap := c.cap
	return &cap, nil
}

func (c *Client) Flags(ctx context.Context) ([]string, error) {
	if c.ErrFlags != nil {
		return nil, c.ErrFlags
	}
	return append([]string(nil), c.flags...), nil
}

func (c *Client) OSDs(ctx context.Context) ([]model.OSD, error) {
	if c.ErrOSDs != nil {
		return nil, c.ErrOSDs
	}
	return append([]model.OSD(nil), c.osds...), nil
}

func (c *Client) Pools(ctx context.Context) ([]model.Pool, error) {
	if c.ErrPools != nil {
		return nil, c.ErrPools
	}
	return append([]model.Pool(nil), c.pools...), nil
}

func (c *Client) CrushTree(ctx context.Context) ([]model.CrushNode, error) {
	return append([]model.CrushNode(nil), c.crush...), nil
}

func (c *Client) CrushRules(ctx context.Context) ([]model.CrushRule, error) {
	return append([]model.CrushRule(nil), c.rules...), nil
}

func (c *Client) Services(ctx context.Context) ([]model.Service, error) {
	return append([]model.Service(nil), c.services...), nil
}

func (c *Client) Daemons(ctx context.Context, serviceName string) ([]model.Daemon, error) {
	return append([]model.Daemon(nil), c.daemons[serviceName]...), nil
}

func (c *Client) PGsByPool(ctx context.Context, pool string) ([]model.PG, error) {
	return append([]model.PG(nil), c.pgs[pool]...), nil
}

func (c *Client) PGs(ctx context.Context) ([]model.PG, error) {
	var all []model.PG
	for _, pgs := range c.pgs {
		all = append(all, pgs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, nil
}

// Admin records the command and applies a simplified state change so the mock
// UI reflects the operation. It intentionally models only what the demo needs.
func (c *Client) Admin(ctx context.Context, command map[string]any) error {
	c.LastCommand = command
	if c.ErrAdmin != nil {
		return c.ErrAdmin
	}

	prefix, _ := command["prefix"].(string)
	switch prefix {
	case "osd out":
		c.eachTargetOSD(command, func(o *model.OSD) { o.In = false; o.Reweight = 0 })
	case "osd in":
		c.eachTargetOSD(command, func(o *model.OSD) { o.In = true; o.Reweight = 1 })
	case "osd reweight":
		if id, ok := toInt(command["id"]); ok {
			if w, ok := command["weight"].(float64); ok {
				c.mutateOSD(id, func(o *model.OSD) { o.Reweight = w; o.In = w > 0 })
			}
		}
	case "osd destroy":
		if id, ok := toInt(command["id"]); ok {
			c.mutateOSD(id, func(o *model.OSD) { o.Up = false; o.In = false; o.Reweight = 0 })
		}
	case "osd purge", "osd rm":
		c.eachTargetOSD(command, func(o *model.OSD) { o.ID = -1 }) // mark for removal
		c.removeTombstoned()
	case "osd pool create":
		c.createPool(command)
	case "osd pool set":
		c.setPoolVar(command)
	case "osd pool application enable":
		c.enablePoolApp(command)
	case "osd pool delete":
		c.deletePool(command)
	case "osd crush move":
		c.crushMove(command)
	case "osd crush reweight":
		c.crushReweight(command)
	case "osd crush add-bucket":
		c.crushAddBucket(command)
	case "osd crush rename-bucket":
		c.crushRename(command)
	case "osd crush remove":
		c.crushRemove(command)
	case "osd crush set-device-class":
		c.crushSetClass(command)
	case "osd set":
		c.setFlag(str(command["key"]))
	case "osd unset":
		c.unsetFlag(str(command["key"]))
	case "orch restart", "orch redeploy":
		c.serviceAllRunning(str(command["service_name"]))
	case "orch daemon":
		c.daemonAction(str(command["action"]), str(command["name"]))
	}
	return nil
}

// serviceAllRunning marks every daemon of a service running (models a completed
// restart/redeploy for the mock).
func (c *Client) serviceAllRunning(name string) {
	list := c.daemons[name]
	for i := range list {
		list[i].Status = "running"
	}
	c.recountService(name)
}

func (c *Client) daemonAction(action, name string) {
	status := "running"
	if action == "stop" {
		status = "stopped"
	}
	for svc, list := range c.daemons {
		for i := range list {
			if list[i].Name == name {
				list[i].Status = status
				c.recountService(svc)
				return
			}
		}
	}
}

// recountService updates a service's running count from its daemons' statuses.
func (c *Client) recountService(name string) {
	running := 0
	for _, d := range c.daemons[name] {
		if d.Status == "running" {
			running++
		}
	}
	for i := range c.services {
		if c.services[i].Name == name {
			c.services[i].Running = running
			return
		}
	}
}

func (c *Client) setFlag(name string) {
	if name == "" {
		return
	}
	for _, f := range c.flags {
		if f == name {
			return
		}
	}
	c.flags = append(c.flags, name)
}

func (c *Client) unsetFlag(name string) {
	kept := c.flags[:0]
	for _, f := range c.flags {
		if f != name {
			kept = append(kept, f)
		}
	}
	c.flags = kept
}

func (c *Client) Close() error { return nil }

// --- helpers -------------------------------------------------------------

func (c *Client) mutateOSD(id int, fn func(*model.OSD)) {
	for i := range c.osds {
		if c.osds[i].ID == id {
			fn(&c.osds[i])
			return
		}
	}
}

// eachTargetOSD applies fn to every OSD named in the command's "id"/"ids".
func (c *Client) eachTargetOSD(command map[string]any, fn func(*model.OSD)) {
	if id, ok := toInt(command["id"]); ok {
		c.mutateOSD(id, fn)
	}
	if ids, ok := command["ids"].([]string); ok {
		for _, s := range ids {
			if id, ok := toInt(s); ok {
				c.mutateOSD(id, fn)
			}
		}
	}
}

func (c *Client) mutatePool(name string, fn func(*model.Pool)) {
	for i := range c.pools {
		if c.pools[i].Name == name {
			fn(&c.pools[i])
			return
		}
	}
}

func (c *Client) createPool(command map[string]any) {
	name, _ := command["pool"].(string)
	if name == "" {
		return
	}
	for _, p := range c.pools {
		if p.Name == name {
			return // already exists
		}
	}
	nextID := 0
	for _, p := range c.pools {
		if p.ID >= nextID {
			nextID = p.ID + 1
		}
	}
	pgNum := 8
	if n, ok := toInt(command["pg_num"]); ok {
		pgNum = n
	}
	rule := "replicated_rule"
	if r, ok := command["rule"].(string); ok && r != "" {
		rule = r
	}
	c.pools = append(c.pools, model.Pool{
		ID: nextID, Name: name, Type: "replicated", Size: 3, MinSize: 2,
		PGNum: pgNum, PGPNum: pgNum, CrushRule: rule, AutoscaleMode: "on",
	})
}

func (c *Client) setPoolVar(command map[string]any) {
	name, _ := command["pool"].(string)
	key, _ := command["var"].(string)
	val, _ := command["val"].(string)
	c.mutatePool(name, func(p *model.Pool) {
		switch key {
		case "size":
			if n, ok := toInt(val); ok {
				p.Size = n
			}
		case "min_size":
			if n, ok := toInt(val); ok {
				p.MinSize = n
			}
		case "pg_num":
			if n, ok := toInt(val); ok {
				p.PGNum = n
				p.PGPNum = n
			}
		case "pg_autoscale_mode":
			p.AutoscaleMode = val
		case "crush_rule":
			p.CrushRule = val
		}
	})
}

func (c *Client) enablePoolApp(command map[string]any) {
	name, _ := command["pool"].(string)
	app, _ := command["app"].(string)
	if app == "" {
		return
	}
	c.mutatePool(name, func(p *model.Pool) {
		for _, a := range p.Applications {
			if a == app {
				return
			}
		}
		p.Applications = append(p.Applications, app)
	})
}

func (c *Client) deletePool(command map[string]any) {
	name, _ := command["pool"].(string)
	kept := c.pools[:0]
	for _, p := range c.pools {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	c.pools = kept
}

// crushTypeIDs maps CRUSH bucket type names to their default numeric ordinals.
var crushTypeIDs = map[string]int{
	"osd": 0, "host": 1, "chassis": 2, "rack": 3, "row": 4, "pdu": 5,
	"pod": 6, "room": 7, "datacenter": 8, "zone": 9, "region": 10, "root": 11,
}

func (c *Client) crushNodeByName(name string) *model.CrushNode {
	for i := range c.crush {
		if c.crush[i].Name == name {
			return &c.crush[i]
		}
	}
	return nil
}

func (c *Client) crushDetach(id int) {
	for i := range c.crush {
		kept := c.crush[i].Children[:0]
		for _, ch := range c.crush[i].Children {
			if ch != id {
				kept = append(kept, ch)
			}
		}
		c.crush[i].Children = kept
	}
}

func (c *Client) crushMove(command map[string]any) {
	node := c.crushNodeByName(str(command["name"]))
	if node == nil {
		return
	}
	args, _ := command["args"].([]string)
	if len(args) == 0 {
		return
	}
	// args[0] is "type=name".
	parts := strings.SplitN(args[0], "=", 2)
	if len(parts) != 2 {
		return
	}
	dest := c.crushNodeByName(parts[1])
	if dest == nil {
		return
	}
	c.crushDetach(node.ID)
	dest.Children = append(dest.Children, node.ID)
}

func (c *Client) crushReweight(command map[string]any) {
	if node := c.crushNodeByName(str(command["name"])); node != nil {
		if w, ok := command["weight"].(float64); ok {
			node.CrushWeight = w
		}
	}
}

func (c *Client) crushAddBucket(command map[string]any) {
	name := str(command["name"])
	if name == "" || c.crushNodeByName(name) != nil {
		return
	}
	nextID := -1
	for _, n := range c.crush {
		if n.ID <= nextID {
			nextID = n.ID - 1
		}
	}
	c.crush = append(c.crush, model.CrushNode{
		ID: nextID, Name: name, Type: str(command["type"]), TypeID: crushTypeIDs[str(command["type"])],
	})
}

func (c *Client) crushRename(command map[string]any) {
	if node := c.crushNodeByName(str(command["srcname"])); node != nil {
		node.Name = str(command["dstname"])
	}
}

func (c *Client) crushRemove(command map[string]any) {
	node := c.crushNodeByName(str(command["name"]))
	if node == nil {
		return
	}
	id := node.ID
	c.crushDetach(id)
	kept := c.crush[:0]
	for _, n := range c.crush {
		if n.ID != id {
			kept = append(kept, n)
		}
	}
	c.crush = kept
}

func (c *Client) crushSetClass(command map[string]any) {
	class := str(command["class"])
	ids, _ := command["ids"].([]string)
	for _, name := range ids {
		if node := c.crushNodeByName(name); node != nil {
			node.DeviceClass = class
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func (c *Client) removeTombstoned() {
	kept := c.osds[:0]
	for _, o := range c.osds {
		if o.ID >= 0 {
			kept = append(kept, o)
		}
	}
	c.osds = kept
}

// toInt coerces the loosely-typed command values (int or numeric string) used
// by Ceph mon commands into an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case string:
		var i int
		if _, err := fmt.Sscanf(n, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

// cloneHealth returns a copy so callers cannot mutate the mock's state.
func (c *Client) cloneHealth() model.Health {
	h := c.health
	h.Checks = append([]model.HealthCheck(nil), c.health.Checks...)
	return h
}
