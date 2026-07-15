// Command siphon is the entrypoint for the Siphon Ceph TUI.
//
// Its only job is composition: load config, select and construct a ceph.Client,
// wire it through the service layer into the UI, and run the Bubble Tea
// program. No business logic lives here.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/ceph"
	"github.com/cinpol/siphon/internal/ceph/goceph"
	"github.com/cinpol/siphon/internal/ceph/mock"
	"github.com/cinpol/siphon/internal/config"
	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui"
	"github.com/cinpol/siphon/internal/version"
)

func main() {
	clientMode := flag.String("client", "auto", "ceph client to use: auto|mock|goceph")
	cephConf := flag.String("ceph-conf", "", "path to ceph.conf (overrides app config)")
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	cfg, err := config.Load()
	if err != nil {
		// Non-fatal: fall back to defaults but tell the operator why.
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	if *cephConf != "" {
		cfg.Ceph.ConfigPath = *cephConf
	}

	client, clientName, err := buildClient(*clientMode, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	svc := service.New(client)
	model := ui.New(svc, cfg.UI.RefreshInterval(), cfg.UI.PoolRows(), cfg.UI.ProblemFlags(), clientName)

	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// buildClient selects the ceph.Client based on the requested mode.
//
//   - "mock":   always the in-memory client (demo data; explicitly requested).
//   - "goceph": the native client; errors if unavailable.
//   - "auto":   the native client, erroring if it is unavailable. It never falls
//     back to the mock, so an operator can never mistake demo data for a live
//     cluster; the mock is only used when explicitly asked for via "mock".
//
// The chosen client's name is returned for display in the header.
func buildClient(mode string, cfg config.Config) (ceph.Client, string, error) {
	switch mode {
	case "mock":
		return mock.New(), "mock", nil

	case "goceph":
		c, err := goceph.New(goceph.Config{ConfigPath: cfg.Ceph.ConfigPath, User: cfg.Ceph.User})
		if err != nil {
			return nil, "", err
		}
		return c, "goceph", nil

	case "auto":
		c, err := goceph.New(goceph.Config{ConfigPath: cfg.Ceph.ConfigPath, User: cfg.Ceph.User})
		if err != nil {
			// Never silently fall back to the mock: an operator must not mistake
			// demo data for a live cluster. Fail with guidance instead.
			return nil, "", fmt.Errorf("native go-ceph client unavailable: %w\n\n"+
				"Fix your ceph.conf/keyring (a working `ceph -s` from this host confirms it), "+
				"or run with --client mock for demo data.", err)
		}
		return c, "goceph", nil

	default:
		return nil, "", fmt.Errorf("unknown client mode %q (want auto|mock|goceph)", mode)
	}
}
