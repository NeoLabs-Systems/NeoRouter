package manager

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/backup"
)

// cmdBackup is `neorouter backup [list]`.
func (m *Manager) cmdBackup(ctx context.Context, args []string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	if positional := cli.Positionals(args); len(positional) > 0 {
		if positional[0] != "list" {
			return inputError("Usage: neorouter " + argSpecs["backup"].usage)
		}
		items, err := backup.List(cfg.Paths.Backups)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			m.UI.Info("No backups yet.")
			return nil
		}
		for _, item := range items {
			fmt.Fprintf(m.Out, "%s  %.1f MB  %s\n", item.Key, float64(item.Bytes)/1e6, item.CreatedAt)
		}
		return nil
	}
	if cfg, err = m.configWithPaths(); err != nil {
		return err
	}
	m.UI.Title("Backing up NeoRouter", "")
	m.UI.Blank()
	var item backup.Artifact
	err = m.UI.RunSteps(false, cli.Step{
		Label: cli.Label{Doing: "Saving your settings and data", Done: "Saved your settings and data"},
		Run: func() (err error) {
			item, err = backup.Create(ctx, backup.InstallOf(cfg.Paths), backup.KindManual)
			return err
		},
	})
	if err != nil {
		return err
	}
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("Backup saved"), item.Path)
	m.UI.Note(fmt.Sprintf("Put it back with %s.", m.UI.Command("neorouter restore "+item.Key)))
	m.UI.Blank()
	return nil
}

// cmdRestore is `neorouter restore <backup>`. The current data is saved first,
// so a restore can itself be undone.
func (m *Manager) cmdRestore(ctx context.Context, args []string) error {
	positional := cli.Positionals(args)
	if len(positional) != 1 {
		return inputError("Usage: neorouter " + argSpecs["restore"].usage)
	}
	cfg, err := m.configWithPaths()
	if err != nil {
		// A restore may be what repairs a broken .env.
		cfg.Paths = m.installPaths()
		if cfg.Port <= 0 {
			cfg.Port = DefaultPort
		}
	}
	item, err := backup.Find(cfg.Paths.Backups, positional[0])
	if err != nil {
		return inputError(err.Error())
	}
	if m.UI.Interactive() {
		ok, err := m.UI.Confirm("Replace the current settings and data with this backup? They are saved first.", false)
		if err != nil || !ok {
			return err
		}
	}
	sm, err := m.serviceManager(cfg)
	if err != nil {
		return err
	}
	install := backup.InstallOf(cfg.Paths)
	wasRunning := cli.PortOpen(cfg.Port) || sm.Active()
	var safety backup.Artifact
	err = m.UI.RunSteps(false,
		cli.Step{Label: cli.Label{Doing: "Stopping NeoRouter", Done: "Stopped NeoRouter"}, Run: func() error {
			if m.inContainer() {
				if cli.PortOpen(cfg.Port) {
					return fmt.Errorf("Stop the NeoRouter container's server before restoring.")
				}
				return nil
			}
			return m.freePort(ctx, cfg)
		}},
		cli.Step{Label: cli.Label{Doing: "Saving the current data first", Done: "Saved the current data"}, Run: func() (err error) {
			safety, err = backup.Create(ctx, install, backup.KindRestore)
			return err
		}},
		cli.Step{Label: cli.Label{Doing: "Restoring the backup", Done: "Restored the backup"}, Run: func() error {
			return backup.Restore(install, item.Key)
		}},
	)
	if err != nil {
		if safety.Path != "" {
			m.UI.Warn("The data from before the restore is in " + safety.Path)
		}
		return err
	}
	m.UI.Blank()
	m.UI.Item("ok", m.UI.Bold("Backup restored"), item.Key)
	m.UI.Note("The data from before the restore is saved as " + safety.Key + ".")
	// A backup from before an upgrade belongs to the old version: starting this
	// one would upgrade it again.
	if item.Kind == backup.KindMigration || isNodeBackup(item) {
		m.UI.Note(fmt.Sprintf("This data is from before the upgrade, so NeoRouter was left stopped. Start the old version, or run %s to upgrade again.",
			m.UI.Command("neorouter install")))
		m.UI.Blank()
		return nil
	}
	if wasRunning && !m.inContainer() {
		return m.cmdStart(ctx, nil)
	}
	m.UI.Blank()
	return nil
}

// isNodeBackup reports a snapshot the Node.js CLI wrote.
func isNodeBackup(item backup.Artifact) bool {
	return !exists(filepath.Join(item.Path, "backup.json"))
}
