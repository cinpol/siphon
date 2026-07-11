package decode

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFixtures runs every decoder against real admin-command JSON captured from
// live clusters, one directory per Ceph release under testdata/. It is the
// behavioural half of "supporting a release" (the build/link half is CI's
// distros workflow): it catches schema drift when Ceph changes an output shape.
//
// Adding a release is just dropping its captured JSON into testdata/<release>/
// (see docs/testing.md for the exact `ceph … -f json` capture commands). The
// directory name must be the release code name, because the version check below
// asserts the decoded release matches it.
func TestFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	ran := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ran = true
		t.Run(e.Name(), func(t *testing.T) {
			runFixtureChecks(t, e.Name())
		})
	}
	if !ran {
		t.Skip("no release fixtures under testdata/")
	}
}

// runFixtureChecks exercises each decoder against one release's fixtures and
// asserts the result is well-formed. The assertions are deliberately structural
// (non-empty, populated key fields) rather than value-exact, so they hold for
// any real cluster's capture while still catching a schema that stopped
// decoding.
func runFixtureChecks(t *testing.T, release string) {
	read := func(file string) []byte {
		t.Helper()
		p := filepath.Join("testdata", release, file)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return raw
	}

	t.Run("health", func(t *testing.T) {
		h, err := Health(read("health.json"))
		if err != nil {
			t.Fatal(err)
		}
		if h.Status == "" {
			t.Error("empty health status")
		}
	})

	t.Run("version", func(t *testing.T) {
		v, err := Version(read("version.json"))
		if err != nil {
			t.Fatal(err)
		}
		if v.Release != release {
			t.Errorf("detected release %q, want %q (the fixture directory name)", v.Release, release)
		}
		if v.Major == 0 {
			t.Error("major version not detected from version string")
		}
	})

	t.Run("status", func(t *testing.T) {
		s, err := Status(read("status.json"))
		if err != nil {
			t.Fatal(err)
		}
		if s.FSID == "" {
			t.Error("empty FSID")
		}
	})

	t.Run("capacity", func(t *testing.T) {
		c, err := Capacity(read("df.json"))
		if err != nil {
			t.Fatal(err)
		}
		if c.TotalBytes <= 0 {
			t.Errorf("total_bytes = %d, want > 0", c.TotalBytes)
		}
	})

	t.Run("pool_usage", func(t *testing.T) {
		usage, err := PoolUsage(read("df.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(usage) == 0 {
			t.Error("no per-pool usage decoded from df")
		}
	})

	t.Run("osds", func(t *testing.T) {
		osds, err := OSDs(read("osd_df_tree.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(osds) == 0 {
			t.Fatal("no OSDs decoded")
		}
		// Regression guard: every OSD must resolve to its host bucket. An empty
		// Host means `osd df tree` did not emit the tree structure (the bug that
		// the goceph output_method:tree argument fixed).
		for _, o := range osds {
			if o.Host == "" {
				t.Errorf("osd.%d has empty Host — osd df tree is not producing host buckets", o.ID)
			}
		}
	})

	t.Run("pgs", func(t *testing.T) {
		pgs, err := PGs(read("pg_ls.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(pgs) == 0 {
			t.Error("no PGs decoded")
		}
	})

	t.Run("services", func(t *testing.T) {
		svcs, err := Services(read("orch_ls.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(svcs) == 0 {
			t.Error("no services decoded from orch ls")
		}
	})

	t.Run("daemons", func(t *testing.T) {
		daemons, err := Daemons(read("orch_ps.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(daemons) == 0 {
			t.Error("no daemons decoded from orch ps")
		}
	})

	t.Run("crush_nodes", func(t *testing.T) {
		nodes, err := CrushNodes(read("osd_crush_tree.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) == 0 {
			t.Error("no CRUSH nodes decoded")
		}
	})

	t.Run("crush_rules", func(t *testing.T) {
		rules, err := CrushRules(read("osd_crush_rule_dump.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(rules) == 0 {
			t.Error("no CRUSH rules decoded")
		}
	})

	t.Run("crush_rule_names", func(t *testing.T) {
		names, err := CrushRuleNames(read("osd_crush_rule_dump.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(names) == 0 {
			t.Error("no CRUSH rule names decoded")
		}
	})

	t.Run("pools", func(t *testing.T) {
		pools, err := Pools(read("osd_dump.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(pools) == 0 {
			t.Error("no pools decoded from osd dump")
		}
	})

	t.Run("flags", func(t *testing.T) {
		// A healthy cluster may have no flags set, so only assert the payload
		// decodes — an unexpected shape would error here.
		if _, err := Flags(read("osd_dump.json")); err != nil {
			t.Fatal(err)
		}
	})
}
