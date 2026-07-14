//go:build goceph

// Package goceph is the native Ceph transport, backed by go-ceph (librados).
//
// It is the ONLY package in Siphon permitted to import go-ceph. Everything
// above it depends on the ceph.Client interface, so this file is where cgo and
// librados live and nowhere else.
//
// Admin operations are issued through librados' MonCommand, which accepts and
// returns the SAME JSON the `ceph` CLI uses — the CLI is itself a thin wrapper
// around this interface. The returned JSON is parsed by the version-aware
// decode package, keeping schema handling out of the transport.
//
// This file requires librados at build time (the goceph tag) and a reachable
// cluster at run time. Its JSON handling is covered by the decode package's
// per-release golden fixtures (internal/ceph/decode/testdata), captured from
// real Reef, Squid and Tentacle clusters.
package goceph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ceph/go-ceph/rados"

	"github.com/cinpol/siphon/internal/ceph"
	"github.com/cinpol/siphon/internal/ceph/decode"
	"github.com/cinpol/siphon/internal/model"
)

// Client is the native go-ceph implementation of ceph.Client.
type Client struct {
	conn *rados.Conn
}

// New establishes a connection to the cluster using the given configuration.
func New(cfg Config) (ceph.Client, error) {
	user := cfg.User
	if user == "" {
		user = "client.admin"
	}
	// librados expects the user without the "client." prefix.
	conn, err := rados.NewConnWithUser(strings.TrimPrefix(user, "client."))
	if err != nil {
		return nil, fmt.Errorf("create rados connection: %w", err)
	}

	if cfg.ConfigPath != "" {
		if err := conn.ReadConfigFile(cfg.ConfigPath); err != nil {
			return nil, fmt.Errorf("read ceph.conf %q: %w", cfg.ConfigPath, err)
		}
	} else if err := conn.ReadDefaultConfigFile(); err != nil {
		return nil, fmt.Errorf("read default ceph.conf: %w", err)
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect to cluster: %w", err)
	}
	return &Client{conn: conn}, nil
}

// monCommand marshals cmd to JSON, sends it as a mon command, and returns the
// raw JSON response for the decode package to parse.
func (c *Client) monCommand(cmd map[string]any) ([]byte, error) {
	buf, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	out, _, err := c.conn.MonCommand(buf)
	if err != nil {
		return nil, fmt.Errorf("mon command %v: %w", cmd["prefix"], err)
	}
	return out, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.conn.GetFSID()
	return err
}

func (c *Client) Status(ctx context.Context) (*model.Status, error) {
	out, err := c.monCommand(map[string]any{"prefix": "status", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Status(out)
}

func (c *Client) HealthDetail(ctx context.Context) (*model.Health, error) {
	out, err := c.monCommand(map[string]any{"prefix": "health", "detail": "detail", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Health(out)
}

func (c *Client) Version(ctx context.Context) (*model.ClusterVersion, error) {
	out, err := c.monCommand(map[string]any{"prefix": "version", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Version(out)
}

func (c *Client) Capacity(ctx context.Context) (*model.Capacity, error) {
	out, err := c.monCommand(map[string]any{"prefix": "df", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Capacity(out)
}

func (c *Client) Flags(ctx context.Context) ([]string, error) {
	out, err := c.monCommand(map[string]any{"prefix": "osd dump", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Flags(out)
}

func (c *Client) OSDs(ctx context.Context) ([]model.OSD, error) {
	out, err := c.monCommand(map[string]any{"prefix": "osd df", "output_method": "tree", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.OSDs(out)
}

// Pools composes pool configuration (`osd dump`), utilisation (`df`) and CRUSH
// rule names (`osd crush rule dump`) into a single enriched list.
func (c *Client) Pools(ctx context.Context) ([]model.Pool, error) {
	dump, err := c.monCommand(map[string]any{"prefix": "osd dump", "format": "json"})
	if err != nil {
		return nil, err
	}
	pools, err := decode.Pools(dump)
	if err != nil {
		return nil, err
	}

	// Enrich with CRUSH rule names (id -> name).
	if ruleRaw, err := c.monCommand(map[string]any{"prefix": "osd crush rule dump", "format": "json"}); err == nil {
		if names, err := decode.CrushRuleNames(ruleRaw); err == nil {
			for i := range pools {
				if id, convErr := strconv.Atoi(pools[i].CrushRule); convErr == nil {
					if name, ok := names[id]; ok {
						pools[i].CrushRule = name
					}
				}
			}
		}
	}

	// Enrich with utilisation from df (keyed by pool name).
	if dfRaw, err := c.monCommand(map[string]any{"prefix": "df", "format": "json"}); err == nil {
		if usage, err := decode.PoolUsage(dfRaw); err == nil {
			for i := range pools {
				if u, ok := usage[pools[i].Name]; ok {
					pools[i].UsedRatio = u.UsedRatio
					pools[i].StoredBytes = u.StoredBytes
					pools[i].Objects = u.Objects
				}
			}
		}
	}

	return pools, nil
}

// PoolUsage returns per-pool utilisation from `df` alone — the lightweight subset
// the dashboard needs, avoiding the osd dump / crush rule dump calls Pools makes.
func (c *Client) PoolUsage(ctx context.Context) ([]model.Pool, error) {
	out, err := c.monCommand(map[string]any{"prefix": "df", "format": "json"})
	if err != nil {
		return nil, err
	}
	usage, err := decode.PoolUsage(out)
	if err != nil {
		return nil, err
	}
	pools := make([]model.Pool, 0, len(usage))
	for name, u := range usage {
		u.Name = name
		pools = append(pools, u)
	}
	// df returns pools in an unspecified order; sort by name for a stable result
	// (the dashboard re-sorts by %used for display).
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools, nil
}

func (c *Client) CrushTree(ctx context.Context) ([]model.CrushNode, error) {
	out, err := c.monCommand(map[string]any{"prefix": "osd crush tree", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.CrushNodes(out)
}

func (c *Client) CrushRules(ctx context.Context) ([]model.CrushRule, error) {
	out, err := c.monCommand(map[string]any{"prefix": "osd crush rule dump", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.CrushRules(out)
}

func (c *Client) Services(ctx context.Context) ([]model.Service, error) {
	out, err := c.monCommand(map[string]any{"prefix": "orch ls", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.Services(out)
}

func (c *Client) Daemons(ctx context.Context, serviceName string) ([]model.Daemon, error) {
	cmd := map[string]any{"prefix": "orch ps", "format": "json"}
	if serviceName != "" {
		cmd["service_name"] = serviceName
	}
	out, err := c.monCommand(cmd)
	if err != nil {
		return nil, err
	}
	return decode.Daemons(out)
}

func (c *Client) PGsByPool(ctx context.Context, pool string) ([]model.PG, error) {
	out, err := c.monCommand(map[string]any{"prefix": "pg ls-by-pool", "poolstr": pool, "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.PGs(out)
}

func (c *Client) PGs(ctx context.Context) ([]model.PG, error) {
	out, err := c.monCommand(map[string]any{"prefix": "pg ls", "format": "json"})
	if err != nil {
		return nil, err
	}
	return decode.PGs(out)
}

// Admin executes a mutating mon command. The service builds the command map; we
// ensure JSON output and discard the (informational) response buffer.
func (c *Client) Admin(ctx context.Context, command map[string]any) error {
	if _, ok := command["format"]; !ok {
		command["format"] = "json"
	}
	_, err := c.monCommand(command)
	return err
}

func (c *Client) Close() error {
	if c.conn != nil {
		c.conn.Shutdown()
	}
	return nil
}
