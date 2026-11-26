package manager

import (
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

// helpEntry is one line of the help: the command as people type it and what it
// does.
type helpEntry struct{ command, description string }

// helpGroup is a titled group of entries.
type helpGroup struct {
	name    string
	entries []helpEntry
}

// helpGroups are the everyday commands, grouped by what someone is trying to
// do (lib/cli/help.js GROUPS).
var helpGroups = []helpGroup{
	{"Getting started", []helpEntry{
		{"install", "Get NeoRouter running on this computer"},
		{"channel", "Choose stable or beta updates"},
	}},
	{"Everyday", []helpEntry{
		{"status", "Is everything working?"},
		{"update", "Get the latest version — your data stays"},
		{"restart", "Turn it off and on again"},
	}},
	{"When something's wrong", []helpEntry{
		{"doctor", "Find problems and explain them"},
		{"repair", "Fix common problems for you"},
		{"logs", "See what NeoRouter is doing right now"},
	}},
	{"Your data", []helpEntry{
		{"backup", "Save a copy of your settings and data"},
		{"start · stop", "Turn NeoRouter on or off"},
		{"uninstall", "Stop NeoRouter starting with your computer"},
	}},
}

// helpReference is every command and flag, for developers and admins
// (lib/cli/help.js REFERENCE).
var helpReference = []helpGroup{
	{"Setup", []helpEntry{
		{"install", "Install dependencies, the web client and the background service"},
		{"setup [--channel stable|beta]", "Prepare the database, settings and web client only"},
	}},
	{"Running", []helpEntry{
		{"start · stop · restart", "Control the background service"},
		{"serve", "Run the server in the foreground"},
		{"status --details", "Health plus install folder, versions and port"},
		{"doctor", "Read-only diagnostics"},
		{"logs", "Follow the server logs"},
		{"backup", "Snapshot settings and data"},
		{"repair", "Reinstall dependencies and the web client, and restart"},
		{"uninstall", "Remove the background service (keeps data)"},
	}},
	{"Updates", []helpEntry{
		{"update [stable|beta]", "Update, optionally switching channel"},
		{"update --notes", "Show what's new without updating"},
		{"channel [stable|beta]", "Show or switch the release channel"},
		{"version", "Print the installed version"},
		{"rebuild-web", "Rebuild the bundled Flutter web client"},
	}},
	{"Accounts & providers", []helpEntry{
		{"reset-password <user> <password>", "Reset an account password"},
		{"reset-2fa <user>", "Remove two-factor authentication from an account"},
		{"providers import <file> --user <user>", "Add provider keys from a JSON file"},
	}},
	{"Configuration", []helpEntry{
		{"env list|get|set|unset", "Read or change settings (secrets masked)"},
	}},
}

// printHelp is lib/cli/help.js printHelp: two columns of groups when the window
// is wide enough, one when it is not.
func (m *Manager) printHelp(all bool) {
	u := m.UI
	if all {
		u.Title("Every neorouter command", "")
		for _, group := range helpReference {
			u.Section(u.Accent(group.name))
			for _, entry := range group.entries {
				u.Print("  " + cli.PadEnd(AppCommand+" "+entry.command, 48) + "  " + u.Muted(entry.description))
			}
		}
		u.Blank()
		return
	}
	u.Title("Here's what you can do", "Not sure? Just type neorouter and pick from a menu.")
	printGroupColumns(u, helpGroups)
	u.Blank()
	u.Print("  " + u.Muted("More for developers and admins:") + " " + u.Command("neorouter help --all"))
	u.Blank()
}

// printGroupColumns lays the groups out in one or two columns, cutting the
// cell text to the window.
func printGroupColumns(u *cli.UI, groups []helpGroup) {
	columns := make([][]string, len(groups))
	columnWidth := 0
	for i, group := range groups {
		column := []string{u.Accent(u.Bold(group.name))}
		for _, entry := range group.entries {
			column = append(column, u.Bold(AppCommand+" "+entry.command), u.Muted(entry.description))
		}
		for _, line := range column {
			columnWidth = max(columnWidth, cli.VisibleLength(line))
		}
		columns[i] = column
	}
	columnWidth += 4
	perRow := 1
	if u.Width() > 2+columnWidth*2 {
		perRow = 2
	}
	for i := 0; i < len(columns); i += perRow {
		left := columns[i]
		var right []string
		if perRow == 2 && i+1 < len(columns) {
			right = columns[i+1]
		}
		u.Blank()
		for row := 0; row < max(len(left), len(right)); row++ {
			cellLeft, cellRight := "", ""
			if row < len(left) {
				cellLeft = left[row]
			}
			if row < len(right) {
				cellRight = right[row]
			}
			u.Print(strings.TrimRight("  "+cli.PadEnd(cellLeft, columnWidth)+cellRight, " "))
		}
	}
}
