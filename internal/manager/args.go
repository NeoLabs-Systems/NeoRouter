package manager

import (
	"slices"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/cli"
	"github.com/NeoLabs-Systems/neorouter/internal/updates"
)

// argSpec is everything a command accepts. A command that changes something
// refuses anything else, so a typo never runs it.
type argSpec struct {
	flags    []string
	valued   []string
	min, max int // positional arguments
	usage    string
}

var argSpecs = map[string]argSpec{
	"install":     {valued: []string{"--channel"}, usage: "install [--channel stable|beta]"},
	"setup":       {valued: []string{"--channel"}, max: 1, usage: "setup [--channel stable|beta]"},
	"start":       {flags: []string{"--foreground"}, usage: "start [--foreground]"},
	"stop":        {usage: "stop"},
	"restart":     {usage: "restart"},
	"status":      {usage: "status [--details]"},
	"doctor":      {usage: "doctor"},
	"repair":      {usage: "repair"},
	"logs":        {usage: "logs"},
	"backup":      {max: 1, usage: "backup [list]"},
	"restore":     {min: 1, max: 1, usage: "restore <backup>   (see neorouter backup list)"},
	"uninstall":   {usage: "uninstall"},
	"update":      {flags: []string{"--notes", "--check"}, max: 1, usage: "update [stable|beta] [--notes|--check]"},
	"channel":     {max: 1, usage: "channel [stable|beta]"},
	"rebuild-web": {usage: "rebuild-web"},
}

// helpRequested reports a help flag, which shows help and changes nothing.
// Commands that take free text only honour it as their first argument.
func helpRequested(command string, args []string) bool {
	if _, strict := argSpecs[command]; !strict && len(args) > 0 {
		args = args[:1]
	}
	return slices.Contains(args, "--help") || slices.Contains(args, "-h")
}

// checkArgs refuses an unknown or mistyped argument. --details is accepted
// everywhere: it adds technical details to a failure.
func checkArgs(command string, args []string) error {
	spec, ok := argSpecs[command]
	if !ok {
		return nil
	}
	refuse := func(problem string) error {
		return inputError(problem + "Usage: neorouter " + spec.usage)
	}
	positional := 0
	for i := 0; i < len(args); i++ {
		name, value, joined := strings.Cut(args[i], "=")
		switch {
		case args[i] == "--details" || slices.Contains(spec.flags, args[i]):
		case slices.Contains(spec.valued, name):
			if !joined && i+1 < len(args) {
				i++
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return refuse(name + " needs a value. ")
			}
		case strings.HasPrefix(args[i], "-"):
			return refuse("Unknown option " + args[i] + ". ")
		default:
			positional++
		}
	}
	if positional < spec.min || positional > spec.max {
		return refuse("")
	}
	return nil
}

// channelArg is the channel a command line names, as `--channel x` or as a bare
// word; "" when it names none.
func channelArg(command string, args []string) (string, error) {
	value, has := cli.FlagValue(args, "--channel")
	if !has {
		if positional := cli.Positionals(args, "--channel"); len(positional) > 0 {
			value, has = positional[0], true
		}
	}
	if !has {
		return "", nil
	}
	channel := updates.ParseChannel(value)
	if channel == "" {
		return "", inputError("Usage: neorouter " + argSpecs[command].usage)
	}
	return channel, nil
}
