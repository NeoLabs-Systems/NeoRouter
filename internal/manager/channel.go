package manager

import (
	"context"
	"fmt"
	"os"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// cmdChannel is `neorouter channel [stable|beta]`: show or switch which releases
// this install follows.
func (m *Manager) cmdChannel(_ context.Context, args []string) error {
	cfg, err := m.ensureDefaultEnvFile()
	if err != nil {
		return err
	}
	positional := cli.Positionals(args)
	if len(positional) == 0 {
		m.UI.Blank()
		m.UI.Item("info", fmt.Sprintf("Updates come from the %s channel.", m.currentChannel(cfg)), "")
		m.UI.Note(fmt.Sprintf("Switch with %s or %s.",
			m.UI.Command("neorouter channel stable"), m.UI.Command("neorouter channel beta")))
		m.UI.Blank()
		return nil
	}
	next := updates.ParseChannel(positional[0])
	if next == "" {
		return inputError("Usage: neorouter channel [stable|beta]")
	}
	if _, err := updates.WriteChannelToEnvFile(next, cfg.Paths.EnvFile); err != nil {
		return err
	}
	_ = os.Setenv(updates.ChannelEnvKey, next)
	m.UI.OK(fmt.Sprintf("Updates now come from the %s channel", next))
	m.UI.Note(fmt.Sprintf("Get it with %s.", m.UI.Command("neorouter update")))
	return nil
}

// cmdBackup is `neorouter backup`: snapshot the settings and data, and keep the
// newest three.
func (m *Manager) cmdBackup(_ context.Context, _ []string) error {
	cfg, err := m.configWithPaths()
	if err != nil {
		return err
	}
	m.UI.Title("Backing up NeoRouter", "")
	m.UI.Blank()
	var target string
	err = m.UI.RunSteps(false, cli.Step{
		Label: cli.Label{Doing: "Saving your settings and data", Done: "Saved your settings and data"},
		Run: func() error {
			artifact, berr := m.makeBackup(cfg)
			target = artifact.Path
			return berr
		},
	})
	if err != nil {
		return err
	}
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("Backup saved"), target)
	m.UI.Blank()
	return nil
}
