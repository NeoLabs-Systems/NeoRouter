package manager

import (
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

// The install's stages, shown as a progress bar above each screen
// (lib/cli/screens.js SETUP_STAGES).
var setupStages = []string{"Welcome", "Install", "Done"}

// markFaces is the NeoRouter mark as a 9×9 dot matrix: the logo's routes
// circling its core (lib/cli/mark.js FACES).
var markFaces = map[string][]string{
	"idle": {
		"....#....", "..#...#..", ".........", "#...#...#", "...###...", "#...#...#", ".........", "..#...#..", "....#....",
	},
	"done": {
		".........", ".......#.", "......#..", ".....#...", "#...#....", ".#.#.....", "..#......", ".........", ".........",
	},
}

// renderMark draws the mark in a rounded rim, gold where lit. Without colors the
// rim is drawn with box characters and the dots with ● and ·.
func renderMark(u *cli.UI, name string) []string {
	rows := markFaces[name]
	if u.Depth == 0 {
		lines := []string{"╭" + strings.Repeat("─", 19) + "╮"}
		for _, row := range rows {
			lines = append(lines, "│ "+strings.Join(dots(row, "●", "·"), " ")+" │")
		}
		return append(lines, "╰"+strings.Repeat("─", 19)+"╯")
	}
	rim := func(text string) string { return u.SGR(u.Foreground("rim"), text) }
	screenColor := u.Background("screen")
	dot := func(cell string) string {
		if cell == "#" {
			return "\x1b[1;" + u.Foreground("accent") + "m●"
		}
		return "\x1b[22;" + u.Foreground("unlit") + "m·"
	}
	screen := func(row string) string {
		parts := make([]string, 0, len(row))
		for _, cell := range row {
			parts = append(parts, dot(string(cell)))
		}
		return "\x1b[" + screenColor + "m " + strings.Join(parts, " ") + " \x1b[0m"
	}
	lines := []string{rim("▗" + strings.Repeat("▄", 19) + "▖")}
	for _, row := range rows {
		lines = append(lines, rim("▐")+screen(row)+rim("▌"))
	}
	return append(lines, rim("▝"+strings.Repeat("▀", 19)+"▘"))
}

// dots maps a row of the matrix onto the lit and unlit characters.
func dots(row, lit, unlit string) []string {
	out := make([]string, 0, len(row))
	for _, cell := range row {
		if cell == '#' {
			out = append(out, lit)
		} else {
			out = append(out, unlit)
		}
	}
	return out
}

// printSetupProgress draws the three install stages (lib/cli/screens.js).
func (m *Manager) printSetupProgress(current int) {
	u := m.UI
	u.Blank()
	if u.IsNarrow() {
		u.Print("  " + u.Faint("Step "+strconv.Itoa(current+1)+" of "+strconv.Itoa(len(setupStages))+" ·") + " " + u.Command(setupStages[current]))
		return
	}
	const segment = 12
	bar := make([]string, len(setupStages))
	labels := make([]string, len(setupStages))
	for i, name := range setupStages {
		if i <= current {
			bar[i] = u.Accent(strings.Repeat("━", segment-1))
		} else {
			bar[i] = u.SGR(u.Foreground("unlit"), strings.Repeat("━", segment-1))
		}
		padded := cli.PadEnd(name, segment)
		switch {
		case i == current:
			labels[i] = u.Command(padded)
		case i < current:
			labels[i] = u.Muted(padded)
		default:
			labels[i] = u.Faint(padded)
		}
	}
	u.Print("  " + strings.Join(bar, " "))
	u.Print(strings.TrimRight("  "+strings.Join(labels, ""), " "))
}

// printSetupFinished is the last install screen: where NeoRouter is and what to
// do there.
func (m *Manager) printSetupFinished(url string, startsItself, hasAccount bool, notes []string) {
	u := m.UI
	u.Blank()
	state := "NeoRouter is running."
	if startsItself {
		state = "NeoRouter starts by itself with your computer."
	}
	next := "and create your account."
	if hasAccount {
		next = "and sign in."
	}
	u.Columns(renderMark(u, "done"), []string{
		u.Bold("You're all set"),
		u.Muted(state),
		"",
		"Open " + u.Accent(url),
		u.Muted(next),
	}, 4)
	if len(notes) > 0 {
		u.Blank()
		for _, note := range notes {
			u.Item("info", note, "")
		}
	}
	u.Blank()
}

// problemsShown is how many problems the home screen lists before pointing at
// `neorouter status`.
const problemsShown = 3

// printHomeHeader is the mark beside one line of state and the address, then
// only the checks that need attention.
func (m *Manager) printHomeHeader(facts statusFacts, problems []statusCheck) {
	u := m.UI
	lines := []string{
		u.Bold("NeoRouter isn't running"),
		u.Faint("run") + " " + u.Command("neorouter start"),
	}
	if facts.Running {
		lines = []string{u.Bold("NeoRouter is running"), u.Accent(facts.URL)}
	}
	u.Blank()
	u.Columns(renderMark(u, "idle"), lines, 4)
	if len(problems) == 0 {
		return
	}
	u.Blank()
	shown := problems
	if len(shown) > problemsShown {
		shown = shown[:problemsShown]
	}
	width := 0
	for _, problem := range shown {
		width = max(width, cli.VisibleLength(problem.Label))
	}
	width += 2
	for _, problem := range shown {
		u.ItemWidth(problem.Tone, u.Bold(problem.Label), problem.Detail, width)
	}
	if len(problems) > len(shown) {
		u.Print("    " + u.Faint("and "+strconv.Itoa(len(problems)-len(shown))+" more —") + " " + u.Command("neorouter status"))
	}
}
