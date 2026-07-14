package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cinpol/siphon/internal/ceph/mock"
	"github.com/cinpol/siphon/internal/model"
)

func TestDashboardAggregates(t *testing.T) {
	svc := New(mock.New())

	d, err := svc.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard error: %v", err)
	}
	if d.Version.Release != "tentacle" {
		t.Errorf("expected tentacle, got %q", d.Version.Release)
	}
	if d.Capacity.TotalBytes == 0 {
		t.Error("expected capacity to be populated")
	}
	if len(d.Flags) == 0 {
		t.Error("expected flags to be populated")
	}
	if len(d.Pools) == 0 {
		t.Error("expected per-pool usage to be populated")
	}
	if len(d.Unavailable) != 0 {
		t.Errorf("expected all sections available, got %v", d.Unavailable)
	}
}

func TestDashboardPoolUsageFailure(t *testing.T) {
	mc := mock.New()
	mc.ErrPools = errors.New("df pools timed out")

	d, err := New(mc).Dashboard(context.Background())
	if err != nil {
		t.Fatalf("dashboard should not hard-fail when pool usage fails: %v", err)
	}
	if d.SectionOK("pools") {
		t.Errorf("pools should be marked unavailable: %v", d.Unavailable)
	}
	if len(d.Pools) != 0 {
		t.Error("expected no pool usage when the df sub-call fails")
	}
}

func TestDashboardPartialFailure(t *testing.T) {
	mc := mock.New()
	// Capacity and flags fail, but the core status call still succeeds.
	mc.ErrCapacity = errors.New("df timed out")
	mc.ErrFlags = errors.New("osd dump timed out")

	d, err := New(mc).Dashboard(context.Background())
	if err != nil {
		t.Fatalf("dashboard should not hard-fail on partial errors: %v", err)
	}
	// Core sections (from status) are still present.
	if d.Health.Status == "" {
		t.Error("expected health to be present")
	}
	if d.SectionOK("capacity") || d.SectionOK("flags") {
		t.Errorf("capacity/flags should be marked unavailable: %v", d.Unavailable)
	}
	if !d.SectionOK("version") {
		t.Error("version should still be available")
	}
}

func TestDashboardCoreFailureIsFatal(t *testing.T) {
	mc := mock.New()
	mc.ErrStatus = errors.New("mon unreachable")

	if _, err := New(mc).Dashboard(context.Background()); err == nil {
		t.Error("expected an error when the core status call fails")
	}
}

// TestOSDOperationCatalogue checks that each operation carries a sensible
// equivalent command, the right danger classification, and — when run —
// dispatches the matching mon-command prefix to the cluster.
func TestOSDOperationCatalogue(t *testing.T) {
	cases := []struct {
		name         string
		op           func(*Service) Operation
		wantCmd      string
		wantPrefix   string
		irreversible bool
	}{
		{"out", func(s *Service) Operation { return s.OSDMarkOut(12) }, "ceph osd out 12", "osd out", false},
		{"in", func(s *Service) Operation { return s.OSDMarkIn(12) }, "ceph osd in 12", "osd in", false},
		{"reweight", func(s *Service) Operation { return s.OSDReweight(12, 0.8) }, "ceph osd reweight 12 0.80", "osd reweight", false},
		{"destroy", func(s *Service) Operation { return s.OSDDestroy(12) }, "ceph osd destroy 12 --yes-i-really-mean-it", "osd destroy", true},
		{"purge", func(s *Service) Operation { return s.OSDPurge(12) }, "ceph osd purge 12 --yes-i-really-mean-it", "osd purge", true},
		{"remove", func(s *Service) Operation { return s.OSDRemove(12) }, "ceph osd rm 12", "osd rm", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := mock.New()
			svc := New(mc)
			op := tc.op(svc)

			if op.Command != tc.wantCmd {
				t.Errorf("command = %q, want %q", op.Command, tc.wantCmd)
			}
			if op.Irreversible != tc.irreversible {
				t.Errorf("irreversible = %v, want %v", op.Irreversible, tc.irreversible)
			}
			if !strings.Contains(op.Consequence, ".") {
				t.Errorf("expected a consequence sentence, got %q", op.Consequence)
			}

			if err := op.Run(context.Background()); err != nil {
				t.Fatalf("Run error: %v", err)
			}
			if got := mc.LastCommand["prefix"]; got != tc.wantPrefix {
				t.Errorf("dispatched prefix = %v, want %q", got, tc.wantPrefix)
			}
		})
	}
}

func TestPoolCreate(t *testing.T) {
	mc := mock.New()
	svc := New(mc)

	op := svc.PoolCreate(PoolCreateSpec{
		Name: "backups", Autoscale: "on", Size: 3, MinSize: 2,
		CrushRule: "replicated_rule", Application: "rbd",
	})
	// Multi-step: create + autoscale + size + min_size + application.
	if lines := strings.Count(op.Command, "\n") + 1; lines != 5 {
		t.Errorf("expected 5 command lines, got %d:\n%s", lines, op.Command)
	}
	if !strings.Contains(op.Command, "ceph osd pool create backups replicated") {
		t.Errorf("missing create line:\n%s", op.Command)
	}

	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	pools, _ := svc.Pools(context.Background())
	if !hasPool(pools, "backups") {
		t.Error("expected 'backups' pool to exist after create")
	}
}

func TestPoolEditOnlyChanges(t *testing.T) {
	svc := New(mock.New())
	pools, _ := svc.Pools(context.Background())
	rbd := findPool(t, pools, "rbd") // size 3, min 2, pg_num 128, autoscale on

	// No changes -> Empty operation.
	same := svc.PoolEdit(rbd, PoolEditSpec{Size: 3, MinSize: 2, PGNum: 128, Autoscale: "on", CrushRule: "replicated_rule"})
	if !same.Empty() {
		t.Errorf("expected empty op for unchanged spec, got commands:\n%s", same.Command)
	}

	// Only size changes -> exactly one step.
	one := svc.PoolEdit(rbd, PoolEditSpec{Size: 4, MinSize: 2, PGNum: 128, Autoscale: "on", CrushRule: "replicated_rule"})
	if one.Empty() || strings.Contains(one.Command, "\n") {
		t.Errorf("expected exactly one command, got:\n%s", one.Command)
	}
	if !strings.Contains(one.Command, "size 4") {
		t.Errorf("expected size change, got:\n%s", one.Command)
	}
}

func TestPoolDelete(t *testing.T) {
	mc := mock.New()
	svc := New(mc)

	op := svc.PoolDelete("rbd")
	if !op.Irreversible {
		t.Errorf("expected pool delete to be irreversible: %+v", op)
	}
	if err := op.Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	pools, _ := svc.Pools(context.Background())
	if hasPool(pools, "rbd") {
		t.Error("expected 'rbd' pool to be gone after delete")
	}
}

func TestPoolDeletePermissionHint(t *testing.T) {
	mc := mock.New()
	svc := New(mc)

	// A permission (EPERM) failure is translated to the mon_allow_pool_delete hint.
	mc.ErrAdmin = errors.New("mon command osd pool delete: rados: ret=-1, Operation not permitted")
	err := svc.PoolDelete("rbd").Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "mon_allow_pool_delete") {
		t.Fatalf("expected the mon_allow_pool_delete hint, got: %v", err)
	}

	// A non-permission failure passes through unchanged.
	mc.ErrAdmin = errors.New("some other cluster failure")
	err = svc.PoolDelete("rbd").Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "some other cluster failure") {
		t.Errorf("non-permission error should pass through, got: %v", err)
	}
}

func TestCrushOperationCatalogue(t *testing.T) {
	cases := []struct {
		name       string
		op         func(*Service) Operation
		wantCmd    string
		wantPrefix string
	}{
		{"move", func(s *Service) Operation { return s.CrushMove("ceph-02", "rack", "rack2") }, "ceph osd crush move ceph-02 rack=rack2", "osd crush move"},
		{"reweight", func(s *Service) Operation { return s.CrushReweight("osd.0", 2.0) }, "ceph osd crush reweight osd.0 2.0000", "osd crush reweight"},
		{"create", func(s *Service) Operation { return s.CrushCreateBucket("rack9", "rack") }, "ceph osd crush add-bucket rack9 rack", "osd crush add-bucket"},
		{"rename", func(s *Service) Operation { return s.CrushRenameBucket("rack1", "rackA") }, "ceph osd crush rename-bucket rack1 rackA", "osd crush rename-bucket"},
		{"delete", func(s *Service) Operation { return s.CrushRemoveBucket("rack2") }, "ceph osd crush remove rack2", "osd crush remove"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := mock.New()
			op := tc.op(New(mc))
			if op.Command != tc.wantCmd {
				t.Errorf("command = %q, want %q", op.Command, tc.wantCmd)
			}
			if err := op.Run(context.Background()); err != nil {
				t.Fatalf("Run error: %v", err)
			}
			if got := mc.LastCommand["prefix"]; got != tc.wantPrefix {
				t.Errorf("dispatched prefix = %v, want %q", got, tc.wantPrefix)
			}
		})
	}
}

func TestCrushSetDeviceClassIsTwoStep(t *testing.T) {
	op := New(mock.New()).CrushSetDeviceClass("osd.3", "ssd")
	// rm-device-class then set-device-class.
	lines := strings.Split(op.Command, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 command lines, got %d:\n%s", len(lines), op.Command)
	}
	if !strings.Contains(lines[0], "rm-device-class") || !strings.Contains(lines[1], "set-device-class ssd osd.3") {
		t.Errorf("unexpected steps:\n%s", op.Command)
	}
}

func TestServiceOperationCatalogue(t *testing.T) {
	cases := []struct {
		name       string
		op         func(*Service) Operation
		wantCmd    string
		wantPrefix string
	}{
		{"svc-restart", func(s *Service) Operation { return s.ServiceRestart("mon") }, "ceph orch restart mon", "orch restart"},
		{"svc-redeploy", func(s *Service) Operation { return s.ServiceRedeploy("mgr") }, "ceph orch redeploy mgr", "orch redeploy"},
		{"daemon-restart", func(s *Service) Operation { return s.DaemonRestart("mon.ceph-01") }, "ceph orch daemon restart mon.ceph-01", "orch daemon"},
		{"daemon-start", func(s *Service) Operation { return s.DaemonStart("mon.ceph-01") }, "ceph orch daemon start mon.ceph-01", "orch daemon"},
		{"daemon-stop", func(s *Service) Operation { return s.DaemonStop("mon.ceph-01") }, "ceph orch daemon stop mon.ceph-01", "orch daemon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := mock.New()
			op := tc.op(New(mc))
			if op.Command != tc.wantCmd {
				t.Errorf("command = %q, want %q", op.Command, tc.wantCmd)
			}
			if err := op.Run(context.Background()); err != nil {
				t.Fatalf("Run error: %v", err)
			}
			if got := mc.LastCommand["prefix"]; got != tc.wantPrefix {
				t.Errorf("prefix = %v, want %q", got, tc.wantPrefix)
			}
		})
	}
}

func TestDaemonStopUpdatesMockState(t *testing.T) {
	mc := mock.New()
	svc := New(mc)

	if err := svc.DaemonStop("mon.ceph-01").Run(context.Background()); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	dmns, _ := svc.Daemons(context.Background(), "mon")
	for _, d := range dmns {
		if d.Name == "mon.ceph-01" && d.Status != "stopped" {
			t.Errorf("expected mon.ceph-01 stopped, got %q", d.Status)
		}
	}
	svcs, _ := svc.Services(context.Background())
	for _, s := range svcs {
		if s.Name == "mon" && s.Running != 2 {
			t.Errorf("expected mon running count 2 after stop, got %d", s.Running)
		}
	}
}

func TestFlagOperations(t *testing.T) {
	svc := New(mock.New())

	if len(svc.FlagCatalogue()) == 0 {
		t.Fatal("expected a non-empty flag catalogue")
	}

	set := svc.FlagSet("noout")
	if set.Command != "ceph osd set noout" {
		t.Errorf("unexpected set op: %+v", set)
	}
	if set.Consequence == "" {
		t.Error("expected a risk/consequence for noout")
	}

	unset := svc.FlagUnset("pause")
	if unset.Command != "ceph osd unset pause" {
		t.Errorf("unexpected unset command: %q", unset.Command)
	}
}

func hasPool(pools []model.Pool, name string) bool {
	for _, p := range pools {
		if p.Name == name {
			return true
		}
	}
	return false
}

func findPool(t *testing.T, pools []model.Pool, name string) model.Pool {
	t.Helper()
	for _, p := range pools {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("pool %q not found", name)
	return model.Pool{}
}
