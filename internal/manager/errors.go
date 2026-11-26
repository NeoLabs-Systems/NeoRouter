package manager

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
)

// inputError is a mistake in what was typed, not a problem with NeoRouter.
func inputError(message string) error {
	return cli.Errorf("CLI_INPUT", "%s", message)
}

// serverNotReadyError is the error a start that never answered carries: the CLI
// adds a hint about the logs for it.
func serverNotReadyError(port int) error {
	return cli.Errorf("SERVER_NOT_READY", "NeoRouter did not become reachable on port %d.", port)
}

// guidance is what a failed command means to the person who ran it.
type guidance struct{ title, next string }

// errorGuidance maps an error code to its headline and the next thing to try.
// CLI_INPUT is plain: the message already says everything, so no "still stuck?"
// line follows it.
var errorGuidance = map[string]guidance{
	"CLI_INPUT":        {"That command didn't work", "neorouter help"},
	"SERVER_NOT_READY": {"NeoRouter started but isn't answering", "neorouter logs"},
}

// PrintError writes a failed command: a headline, what happened and what to try
// next.
func PrintError(w io.Writer, err error, details bool) {
	u := cli.NewUI(false)
	u.Out, u.Err = w, w
	write := func(line string) { fmt.Fprintln(w, line) }
	if errors.Is(err, cli.ErrCancelled) {
		write("\n  " + u.Muted("Cancelled."))
		return
	}
	code := cli.CodeOf(err, "")
	g, known := errorGuidance[code]
	plain := known && code == "CLI_INPUT"
	if !known {
		g = guidance{"Something went wrong", "neorouter doctor"}
	}
	write("")
	write("  " + u.Error("✕") + " " + u.Bold(g.title))
	if message := err.Error(); message != "" {
		for _, line := range strings.Split(message, "\n") {
			write("    " + line)
		}
	}
	write("")
	hint := ""
	if !plain {
		hint = "  ·  still stuck? neorouter repair tries the usual fixes"
	}
	write("  " + u.Muted("Try") + " " + u.Command(g.next) + u.Muted(hint))
	if details {
		write("")
		label := "Technical details:"
		if code != "" {
			label = "Technical details (" + code + "):"
		}
		write("  " + u.Muted(label))
		var lines []string
		for _, line := range strings.Split(err.Error(), "\n") {
			lines = append(lines, "    "+line)
		}
		write(u.Muted(strings.Join(lines, "\n")))
	} else if !plain {
		write("  " + u.Muted("Technical details: add --details"))
	}
	write("")
}

// DetailsRequested reports whether the command line asked for technical details
// on a failure.
func DetailsRequested(args []string) bool { return cli.HasFlag(args, "--details") }
