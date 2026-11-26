package manager

import (
	"context"
	"fmt"

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
	if err := m.setChannel(next); err != nil {
		return err
	}
	m.UI.OK(fmt.Sprintf("Updates now come from the %s channel", next))
	m.UI.Note(fmt.Sprintf("Get it with %s.", m.UI.Command("neorouter update "+next)))
	return nil
}
