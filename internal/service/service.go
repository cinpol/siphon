// Package service holds Siphon's business logic.
//
// It sits between the UI and the ceph.Client transport. The UI never talks to a
// ceph.Client directly; it goes through a Service. This is where operational
// workflows, safety rules (confirmation, equivalent-command preview) and any
// orchestration of multiple cluster calls live, so that no business logic ever
// leaks into the UI or gets duplicated.
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cinpol/siphon/internal/ceph"
	"github.com/cinpol/siphon/internal/model"
)

// Service exposes high-level operations to the UI.
type Service struct {
	client ceph.Client
}

// New wires a Service to a ceph.Client.
func New(client ceph.Client) *Service {
	return &Service{client: client}
}

// Dashboard composes the overview snapshot from several cluster calls. This
// aggregation is deliberately the service's job, not the transport's: the
// client stays aligned with individual Ceph commands while the service decides
// how to combine them for a screen.
//
// It degrades gracefully: `status` is the core call (identity + health + live
// IO), so its failure is fatal for the overview; but if version, capacity or
// flags fail, the dashboard still renders with those sections marked
// unavailable rather than failing the whole screen.
func (s *Service) Dashboard(ctx context.Context) (*model.Dashboard, error) {
	status, err := s.client.Status(ctx)
	if err != nil {
		return nil, err
	}

	d := &model.Dashboard{
		FSID:         status.FSID,
		Health:       status.Health,
		IO:           status.IO,
		Recovery:     status.Recovery,
		Orchestrator: status.Orchestrator,
	}

	// Enrich health with per-check detail lines (`health detail`). If it fails,
	// keep the summary-level health from status rather than failing the screen.
	if hd, err := s.client.HealthDetail(ctx); err == nil {
		d.Health = *hd
	}

	if version, err := s.client.Version(ctx); err == nil {
		d.Version = *version
	} else {
		d.Unavailable = append(d.Unavailable, "version")
	}
	if capacity, err := s.client.Capacity(ctx); err == nil {
		d.Capacity = *capacity
	} else {
		d.Unavailable = append(d.Unavailable, "capacity")
	}
	if flags, err := s.client.Flags(ctx); err == nil {
		d.Flags = flags
	} else {
		d.Unavailable = append(d.Unavailable, "flags")
	}
	if pools, err := s.client.PoolUsage(ctx); err == nil {
		d.Pools = pools
	} else {
		d.Unavailable = append(d.Unavailable, "pools")
	}

	return d, nil
}

// OSDs returns the cluster's OSDs with their placement/utilisation state.
func (s *Service) OSDs(ctx context.Context) ([]model.OSD, error) {
	return s.client.OSDs(ctx)
}

// Pools returns the cluster's storage pools with configuration and utilisation.
func (s *Service) Pools(ctx context.Context) ([]model.Pool, error) {
	return s.client.Pools(ctx)
}

// CrushTree returns the CRUSH hierarchy nodes.
func (s *Service) CrushTree(ctx context.Context) ([]model.CrushNode, error) {
	return s.client.CrushTree(ctx)
}

// CrushRules returns the CRUSH placement rules.
func (s *Service) CrushRules(ctx context.Context) ([]model.CrushRule, error) {
	return s.client.CrushRules(ctx)
}

// Services returns cephadm-managed services.
func (s *Service) Services(ctx context.Context) ([]model.Service, error) {
	return s.client.Services(ctx)
}

// Daemons returns the daemons belonging to a service.
func (s *Service) Daemons(ctx context.Context, serviceName string) ([]model.Daemon, error) {
	return s.client.Daemons(ctx, serviceName)
}

// PGsByPool returns the placement groups of a pool.
func (s *Service) PGsByPool(ctx context.Context, pool string) ([]model.PG, error) {
	return s.client.PGsByPool(ctx, pool)
}

// PGs returns every placement group in the cluster.
func (s *Service) PGs(ctx context.Context) ([]model.PG, error) {
	return s.client.PGs(ctx)
}

// --- Service (orchestrator) operation catalogue --------------------------

// ServiceRestart restarts all daemons of a service.
func (s *Service) ServiceRestart(name string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Restart service %s", name),
		Command:     fmt.Sprintf("ceph orch restart %s", name),
		Consequence: "Restarts every daemon in this service, one at a time. Brief availability impact.",
		steps:       []map[string]any{{"prefix": "orch restart", "service_name": name}},
		client:      s.client,
	}
}

// ServiceRedeploy redeploys all daemons of a service.
func (s *Service) ServiceRedeploy(name string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Redeploy service %s", name),
		Command:     fmt.Sprintf("ceph orch redeploy %s", name),
		Consequence: "Recreates every daemon in this service (e.g. to apply a new image/config). Brief availability impact.",
		steps:       []map[string]any{{"prefix": "orch redeploy", "service_name": name}},
		client:      s.client,
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (s *Service) daemonAction(action, name string) Operation {
	return Operation{
		Title:       fmt.Sprintf("%s daemon %s", capitalize(action), name),
		Command:     fmt.Sprintf("ceph orch daemon %s %s", action, name),
		Consequence: fmt.Sprintf("Runs %q on this single daemon via the orchestrator.", action),
		steps:       []map[string]any{{"prefix": "orch daemon", "action": action, "name": name}},
		client:      s.client,
	}
}

// DaemonRestart restarts a single daemon.
func (s *Service) DaemonRestart(name string) Operation { return s.daemonAction("restart", name) }

// DaemonStart starts a single daemon.
func (s *Service) DaemonStart(name string) Operation { return s.daemonAction("start", name) }

// DaemonStop stops a single daemon (it stays down until started).
func (s *Service) DaemonStop(name string) Operation {
	op := s.daemonAction("stop", name)
	op.Consequence = "Stops this daemon; it stays down until started again. Reduces this service's redundancy."
	return op
}

// Operation is a confirmable cluster mutation. It is the unit the
// safety/confirmation framework works with: the UI shows Command and
// Consequence (and treats Irreversible ones more strictly), asks the operator
// for a y/n confirmation, and calls Run only after they confirm.
//
// An operation may comprise several steps (e.g. creating a pool then enabling
// its application). Command lists one equivalent CLI line per step, so the
// preview always describes exactly what Run executes — they are defined together
// here in the business-logic layer and cannot drift apart.
type Operation struct {
	Title        string // e.g. "Mark OSD.12 out"
	Command      string // equivalent CLI (one line per step), for display
	Consequence  string // what happens, and whether it is reversible
	Irreversible bool

	// PermissionHint, when set, replaces a raw "operation not permitted" (EPERM)
	// failure with actionable guidance — e.g. which cluster setting to enable.
	// It is only used for permission errors; other failures pass through as-is.
	PermissionHint string

	steps  []map[string]any
	client ceph.Client
}

// Empty reports whether the operation has no steps (e.g. an edit with no
// changes), so callers can skip an otherwise no-op confirmation.
func (o Operation) Empty() bool { return len(o.steps) == 0 }

// Run executes each step in order, stopping at the first error. A permission
// (EPERM) failure is translated to PermissionHint when one is set, so the UI can
// tell the operator how to unblock the operation rather than showing a raw
// rados/errno message.
func (o Operation) Run(ctx context.Context) error {
	for _, step := range o.steps {
		if err := o.client.Admin(ctx, step); err != nil {
			if o.PermissionHint != "" && isPermissionError(err) {
				return errors.New(o.PermissionHint)
			}
			return err
		}
	}
	return nil
}

// isPermissionError reports whether err is Ceph's EPERM ("operation not
// permitted") — the errno used by mon guards such as mon_allow_pool_delete.
func isPermissionError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not permitted")
}

// --- OSD operation catalogue ---------------------------------------------

// OSDMarkOut marks an OSD out, draining its data.
func (s *Service) OSDMarkOut(id int) Operation {
	return Operation{
		Title:       fmt.Sprintf("Mark OSD.%d out", id),
		Command:     fmt.Sprintf("ceph osd out %d", id),
		Consequence: "Data will gradually rebalance away from this OSD. Reversible by marking it back in.",
		steps:       []map[string]any{{"prefix": "osd out", "ids": []string{strconv.Itoa(id)}}},
		client:      s.client,
	}
}

// OSDMarkIn marks an OSD in, allowing it to hold data again.
func (s *Service) OSDMarkIn(id int) Operation {
	return Operation{
		Title:       fmt.Sprintf("Mark OSD.%d in", id),
		Command:     fmt.Sprintf("ceph osd in %d", id),
		Consequence: "The OSD will accept data again; PGs will rebalance onto it.",
		steps:       []map[string]any{{"prefix": "osd in", "ids": []string{strconv.Itoa(id)}}},
		client:      s.client,
	}
}

// OSDReweight sets an OSD's override weight (0.0–1.0).
func (s *Service) OSDReweight(id int, weight float64) Operation {
	return Operation{
		Title:       fmt.Sprintf("Reweight OSD.%d to %.2f", id, weight),
		Command:     fmt.Sprintf("ceph osd reweight %d %.2f", id, weight),
		Consequence: "Temporarily adjusts this OSD's weight, moving PGs. Reset by reweighting or marking in.",
		steps:       []map[string]any{{"prefix": "osd reweight", "id": id, "weight": weight}},
		client:      s.client,
	}
}

// OSDDestroy destroys an OSD's data and keys but keeps its ID for reuse.
func (s *Service) OSDDestroy(id int) Operation {
	return Operation{
		Title:        fmt.Sprintf("Destroy OSD.%d", id),
		Command:      fmt.Sprintf("ceph osd destroy %d --yes-i-really-mean-it", id),
		Consequence:  "Destroys this OSD's data and authentication keys, keeping its ID for reuse. NOT reversible.",
		Irreversible: true,
		steps:        []map[string]any{{"prefix": "osd destroy", "id": id, "yes_i_really_mean_it": true}},
		client:       s.client,
	}
}

// OSDPurge removes an OSD completely (CRUSH, auth, osdmap).
func (s *Service) OSDPurge(id int) Operation {
	return Operation{
		Title:        fmt.Sprintf("Purge OSD.%d", id),
		Command:      fmt.Sprintf("ceph osd purge %d --yes-i-really-mean-it", id),
		Consequence:  "Completely removes this OSD from CRUSH, auth and the osdmap. NOT reversible.",
		Irreversible: true,
		steps:        []map[string]any{{"prefix": "osd purge", "id": id, "yes_i_really_mean_it": true}},
		client:       s.client,
	}
}

// OSDRemove removes an OSD from the osdmap (legacy `osd rm`).
func (s *Service) OSDRemove(id int) Operation {
	return Operation{
		Title:        fmt.Sprintf("Remove OSD.%d", id),
		Command:      fmt.Sprintf("ceph osd rm %d", id),
		Consequence:  "Removes this OSD from the osdmap. NOT reversible; use when the OSD is already down and out.",
		Irreversible: true,
		steps:        []map[string]any{{"prefix": "osd rm", "ids": []string{strconv.Itoa(id)}}},
		client:       s.client,
	}
}

// --- Cluster flag catalogue ----------------------------------------------

// FlagInfo is the static operational metadata for a cluster flag: what it does,
// why operators set it, and what it risks. This is domain knowledge, so it lives
// in the business layer rather than being scattered through the UI.
type FlagInfo struct {
	Name        string
	Description string
	Why         string
	Risk        string
}

var flagCatalogue = []FlagInfo{
	{"noout", "OSDs that go down are not automatically marked out.",
		"Set during short maintenance so the cluster doesn't rebalance while an OSD or host is briefly down.",
		"If a down OSD stays down long-term, its data is never re-replicated — redundancy silently degrades."},
	{"norebalance", "Suppress rebalancing of placement groups.",
		"Avoid data movement during maintenance or while investigating an issue.",
		"PG distribution stays uneven; capacity skew can build up while set."},
	{"nobackfill", "Suppress backfill (bulk PG data movement).",
		"Pause heavy background data movement to protect client performance.",
		"Degraded and misplaced PGs are not healed via backfill while this is set."},
	{"norecover", "Suppress recovery operations.",
		"Pause recovery to reduce load during controlled maintenance.",
		"Degraded PGs are NOT recovered — data stays under-replicated. High risk if left on."},
	{"noscrub", "Disable regular (light) scrubbing.",
		"Reduce background IO during peak hours or maintenance.",
		"Latent metadata/data inconsistencies may go undetected for longer."},
	{"nodeep-scrub", "Disable deep scrubbing.",
		"Deep scrubs read all data and are IO-heavy; pause them during peak load.",
		"Bit-rot and deep inconsistencies may go undetected for longer."},
	{"nosnaptrim", "Disable snapshot trimming.",
		"Pause snap-trim IO when it is impacting client latency.",
		"Space from deleted snapshots is not reclaimed while set."},
	{"pause", "Pause all client IO (sets pauserd and pausewr).",
		"Freeze the cluster during critical maintenance or investigation.",
		"ALL client reads and writes stop immediately — applications will hang."},
	{"noup", "Prevent OSDs from being marked up.",
		"Hold OSDs down during a controlled procedure.",
		"OSDs cannot rejoin the cluster; availability and redundancy are reduced."},
	{"noin", "Prevent OSDs from being marked in.",
		"Keep new or returning OSDs out of data placement until you're ready.",
		"Affected OSDs won't accept data; capacity and rebalancing are impacted."},
}

// FlagCatalogue returns the known cluster flags with their metadata.
func (s *Service) FlagCatalogue() []FlagInfo {
	return append([]FlagInfo(nil), flagCatalogue...)
}

// Flags returns the currently-set cluster flags.
func (s *Service) Flags(ctx context.Context) ([]string, error) {
	return s.client.Flags(ctx)
}

func flagRisk(name string) string {
	for _, f := range flagCatalogue {
		if f.Name == name {
			return f.Risk
		}
	}
	return ""
}

// FlagSet sets a cluster flag (reversible; confirmed with y/N).
func (s *Service) FlagSet(name string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Set flag %s", name),
		Command:     fmt.Sprintf("ceph osd set %s", name),
		Consequence: flagRisk(name),
		steps:       []map[string]any{{"prefix": "osd set", "key": name}},
		client:      s.client,
	}
}

// FlagUnset clears a cluster flag (reversible; confirmed with y/N).
func (s *Service) FlagUnset(name string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Unset flag %s", name),
		Command:     fmt.Sprintf("ceph osd unset %s", name),
		Consequence: "Clears the flag, restoring the cluster's normal behaviour for it.",
		steps:       []map[string]any{{"prefix": "osd unset", "key": name}},
		client:      s.client,
	}
}

// --- CRUSH operation catalogue -------------------------------------------

// CrushMove relocates a node under a destination bucket (given as its CRUSH
// type and name, e.g. type "rack", name "rack2").
func (s *Service) CrushMove(nodeName, destType, destName string) Operation {
	loc := destType + "=" + destName
	return Operation{
		Title:       fmt.Sprintf("Move %s under %s", nodeName, destName),
		Command:     fmt.Sprintf("ceph osd crush move %s %s", nodeName, loc),
		Consequence: "Relocates the node in the CRUSH map. Placement changes trigger data movement.",
		steps:       []map[string]any{{"prefix": "osd crush move", "name": nodeName, "args": []string{loc}}},
		client:      s.client,
	}
}

// CrushReweight sets the CRUSH weight of a bucket or OSD.
func (s *Service) CrushReweight(name string, weight float64) Operation {
	return Operation{
		Title:       fmt.Sprintf("Reweight %s to %.4f", name, weight),
		Command:     fmt.Sprintf("ceph osd crush reweight %s %.4f", name, weight),
		Consequence: "Changes the CRUSH weight, moving data proportionally.",
		steps:       []map[string]any{{"prefix": "osd crush reweight", "name": name, "weight": weight}},
		client:      s.client,
	}
}

// CrushCreateBucket adds an empty bucket of the given type.
func (s *Service) CrushCreateBucket(name, bucketType string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Create %s bucket %q", bucketType, name),
		Command:     fmt.Sprintf("ceph osd crush add-bucket %s %s", name, bucketType),
		Consequence: "Adds an empty bucket to the CRUSH map. No data moves until nodes are placed under it.",
		steps:       []map[string]any{{"prefix": "osd crush add-bucket", "name": name, "type": bucketType}},
		client:      s.client,
	}
}

// CrushRenameBucket renames a bucket.
func (s *Service) CrushRenameBucket(src, dst string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Rename %q to %q", src, dst),
		Command:     fmt.Sprintf("ceph osd crush rename-bucket %s %s", src, dst),
		Consequence: "Renames the bucket. No data moves.",
		steps:       []map[string]any{{"prefix": "osd crush rename-bucket", "srcname": src, "dstname": dst}},
		client:      s.client,
	}
}

// CrushRemoveBucket removes a bucket from the CRUSH map (it must be empty).
func (s *Service) CrushRemoveBucket(name string) Operation {
	return Operation{
		Title:        fmt.Sprintf("Delete bucket %q", name),
		Command:      fmt.Sprintf("ceph osd crush remove %s", name),
		Consequence:  "Removes the bucket from the CRUSH map. It must be empty. NOT reversible.",
		Irreversible: true,
		steps:        []map[string]any{{"prefix": "osd crush remove", "name": name}},
		client:       s.client,
	}
}

// CrushSetDeviceClass reassigns an OSD's device class. Ceph requires clearing
// the existing class first, so this is a two-step operation.
func (s *Service) CrushSetDeviceClass(osdName, class string) Operation {
	op := Operation{
		Title:       fmt.Sprintf("Set %s device-class to %q", osdName, class),
		Consequence: "Reassigns the OSD's device class, which can change CRUSH-rule and pool placement.",
		client:      s.client,
	}
	op.addStep(fmt.Sprintf("ceph osd crush rm-device-class %s", osdName),
		map[string]any{"prefix": "osd crush rm-device-class", "ids": []string{osdName}})
	op.addStep(fmt.Sprintf("ceph osd crush set-device-class %s %s", class, osdName),
		map[string]any{"prefix": "osd crush set-device-class", "class": class, "ids": []string{osdName}})
	return op
}

// --- Pool operation catalogue --------------------------------------------

// PoolCreateSpec describes a new replicated pool. Erasure-coded creation is
// planned (it needs EC-profile management).
type PoolCreateSpec struct {
	Name        string
	Size        int
	MinSize     int
	PGNum       int
	Autoscale   string // "on" | "off" | "warn"
	CrushRule   string
	Application string
}

// PoolEditSpec is the desired state of a pool's editable properties. PoolEdit
// diffs it against the current pool and only emits steps for what changed. It
// mirrors PoolCreateSpec (minus Name) so the create and edit forms are identical.
type PoolEditSpec struct {
	Size        int
	MinSize     int
	PGNum       int
	Autoscale   string // "on" | "off" | "warn"
	CrushRule   string
	Application string
}

func poolSetStep(name, key, val string) map[string]any {
	return map[string]any{"prefix": "osd pool set", "pool": name, "var": key, "val": val}
}

// PoolCreate builds the multi-step operation that creates a replicated pool.
func (s *Service) PoolCreate(spec PoolCreateSpec) Operation {
	op := Operation{
		Title:       fmt.Sprintf("Create pool %q", spec.Name),
		Consequence: "Creates a new replicated pool. PGs are allocated and data placement begins immediately.",
		client:      s.client,
	}

	create := map[string]any{"prefix": "osd pool create", "pool": spec.Name, "pool_type": "replicated"}
	createLine := fmt.Sprintf("ceph osd pool create %s replicated", spec.Name)
	if spec.PGNum > 0 {
		create["pg_num"] = spec.PGNum
		create["pgp_num"] = spec.PGNum
		createLine = fmt.Sprintf("ceph osd pool create %s %d %d replicated", spec.Name, spec.PGNum, spec.PGNum)
	}
	if spec.CrushRule != "" {
		create["rule"] = spec.CrushRule
		createLine += " " + spec.CrushRule
	}
	op.addStep(createLine, create)

	mode := spec.Autoscale
	if mode == "" {
		mode = "on"
	}
	op.addStep(
		fmt.Sprintf("ceph osd pool set %s pg_autoscale_mode %s", spec.Name, mode),
		poolSetStep(spec.Name, "pg_autoscale_mode", mode),
	)
	if spec.Size > 0 {
		op.addStep(fmt.Sprintf("ceph osd pool set %s size %d", spec.Name, spec.Size),
			poolSetStep(spec.Name, "size", strconv.Itoa(spec.Size)))
	}
	if spec.MinSize > 0 {
		op.addStep(fmt.Sprintf("ceph osd pool set %s min_size %d", spec.Name, spec.MinSize),
			poolSetStep(spec.Name, "min_size", strconv.Itoa(spec.MinSize)))
	}
	if spec.Application != "" {
		op.addStep(fmt.Sprintf("ceph osd pool application enable %s %s", spec.Name, spec.Application),
			map[string]any{"prefix": "osd pool application enable", "pool": spec.Name, "app": spec.Application})
	}
	return op
}

// PoolEdit builds an operation containing only the changes between the current
// pool and the desired spec. If nothing changed, the operation is Empty().
func (s *Service) PoolEdit(current model.Pool, spec PoolEditSpec) Operation {
	op := Operation{
		Title:       fmt.Sprintf("Edit pool %q", current.Name),
		Consequence: "Applies the changed pool settings. Size, PG and rule changes trigger data movement.",
		client:      s.client,
	}

	if spec.Size > 0 && spec.Size != current.Size {
		op.addStep(fmt.Sprintf("ceph osd pool set %s size %d", current.Name, spec.Size),
			poolSetStep(current.Name, "size", strconv.Itoa(spec.Size)))
	}
	if spec.MinSize > 0 && spec.MinSize != current.MinSize {
		op.addStep(fmt.Sprintf("ceph osd pool set %s min_size %d", current.Name, spec.MinSize),
			poolSetStep(current.Name, "min_size", strconv.Itoa(spec.MinSize)))
	}
	if spec.PGNum > 0 && spec.PGNum != current.PGNum {
		op.addStep(fmt.Sprintf("ceph osd pool set %s pg_num %d", current.Name, spec.PGNum),
			poolSetStep(current.Name, "pg_num", strconv.Itoa(spec.PGNum)))
	}
	if spec.Autoscale != "" && spec.Autoscale != current.AutoscaleMode {
		op.addStep(fmt.Sprintf("ceph osd pool set %s pg_autoscale_mode %s", current.Name, spec.Autoscale),
			poolSetStep(current.Name, "pg_autoscale_mode", spec.Autoscale))
	}
	if spec.CrushRule != "" && spec.CrushRule != current.CrushRule {
		op.addStep(fmt.Sprintf("ceph osd pool set %s crush_rule %s", current.Name, spec.CrushRule),
			poolSetStep(current.Name, "crush_rule", spec.CrushRule))
	}
	// Application is enable-only from the form: if the named app isn't already on
	// the pool, add it. (Removing an application is a separate, later feature.)
	if spec.Application != "" && !containsString(current.Applications, spec.Application) {
		op.addStep(fmt.Sprintf("ceph osd pool application enable %s %s", current.Name, spec.Application),
			map[string]any{"prefix": "osd pool application enable", "pool": current.Name, "app": spec.Application})
	}
	return op
}

// --- Placement-group operation catalogue ---------------------------------

// PGScrub schedules a light scrub of a placement group.
func (s *Service) PGScrub(id string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Scrub PG %s", id),
		Command:     fmt.Sprintf("ceph pg scrub %s", id),
		Consequence: "Schedules a light scrub (metadata/checksum consistency check). Low impact; no data change.",
		steps:       []map[string]any{{"prefix": "pg scrub", "pgid": id}},
		client:      s.client,
	}
}

// PGDeepScrub schedules a deep scrub of a placement group.
func (s *Service) PGDeepScrub(id string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Deep-scrub PG %s", id),
		Command:     fmt.Sprintf("ceph pg deep-scrub %s", id),
		Consequence: "Schedules a deep scrub (reads and compares all replicas). More IO-intensive; no data change.",
		steps:       []map[string]any{{"prefix": "pg deep-scrub", "pgid": id}},
		client:      s.client,
	}
}

// PGRepair schedules a repair of a placement group.
func (s *Service) PGRepair(id string) Operation {
	return Operation{
		Title:       fmt.Sprintf("Repair PG %s", id),
		Command:     fmt.Sprintf("ceph pg repair %s", id),
		Consequence: "Attempts to repair detected inconsistencies in this PG (may rewrite replicas). Use after a scrub reports errors.",
		steps:       []map[string]any{{"prefix": "pg repair", "pgid": id}},
		client:      s.client,
	}
}

// containsString reports whether xs contains v.
func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// PoolDelete builds the (irreversible) pool deletion operation.
func (s *Service) PoolDelete(name string) Operation {
	return Operation{
		Title:        fmt.Sprintf("Delete pool %q", name),
		Command:      fmt.Sprintf("ceph osd pool delete %s %s --yes-i-really-really-mean-it", name, name),
		Consequence:  "Permanently deletes the pool and ALL of its data. NOT reversible. Requires mon_allow_pool_delete=true.",
		Irreversible: true,
		PermissionHint: "pool deletion is disabled on this cluster. Enable it first with:  " +
			"ceph config set mon mon_allow_pool_delete true  (then disable it again afterwards).",
		steps: []map[string]any{{
			"prefix": "osd pool delete", "pool": name, "pool2": name,
			"yes_i_really_really_mean_it": true,
		}},
		client: s.client,
	}
}

// addStep appends a command line and its payload, keeping the two in lockstep.
func (o *Operation) addStep(commandLine string, payload map[string]any) {
	if o.Command == "" {
		o.Command = commandLine
	} else {
		o.Command += "\n" + commandLine
	}
	o.steps = append(o.steps, payload)
}

// Close releases the underlying client.
func (s *Service) Close() error {
	return s.client.Close()
}
