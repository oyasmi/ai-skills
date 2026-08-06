package app

import (
	"flag"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/harnessarg"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

func parseSummonArgs(args []string) (service.SummonInput, error) {
	in := service.SummonInput{}
	args = splitEqualsForms(args, "--template", "--name", "--cwd", "--model", "--effort", "--command", "--system-prompt", "--prompt")
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--template":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --template")
			}
			in.TemplateName = args[i]
		case "--name":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --name")
			}
			in.Name = args[i]
		case "--cwd":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --cwd")
			}
			in.CWD = &args[i]
		case "--model":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --model")
			}
			in.Model = &args[i]
		case "--effort":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --effort")
			}
			// Reject a bad level here rather than at launch: an unknown level
			// would otherwise fall through to the harness default, which looks
			// like the override worked.
			if !harnessarg.ValidLevel(args[i]) {
				return in, apperr.New("invalid_arguments", "invalid value for --effort: must be one of "+harnessarg.LevelList())
			}
			in.Effort = &args[i]
		case "--command":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --command")
			}
			in.Command = &args[i]
		case "--system-prompt":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --system-prompt")
			}
			in.SystemPrompt = &args[i]
		case "--prompt":
			i++
			if i >= len(args) {
				return in, apperr.New("invalid_arguments", "missing value for --prompt")
			}
			in.Prompt = &args[i]
		default:
			if args[i] == "--json" {
				continue
			}
			if strings.HasPrefix(args[i], "-") {
				return in, apperr.New("invalid_arguments", "unknown flag: "+args[i])
			}
			return in, apperr.New("invalid_arguments", "unexpected argument: "+args[i])
		}
	}
	if strings.TrimSpace(in.TemplateName) == "" {
		return in, apperr.New("invalid_arguments", "summon requires --template\n\n"+summonHelp())
	}
	return in, nil
}

// parseRunArgs accepts summon's flags plus the ones that shape a single
// delegated task.
func parseRunArgs(args []string) (service.RunInput, bool, error) {
	in := service.RunInput{
		TimeoutMS: 5 * 60 * 1000,
		Capture:   capture.Options{History: -1, Scope: capture.ScopeCurrent},
	}
	var promptFile, timeoutRaw string
	useStdin := false
	rest := make([]string, 0, len(args))
	args = splitEqualsForms(args, "--prompt-file", "--timeout", "--history")
	for i := 0; i < len(args); i++ {
		if name, value, ok := splitBooleanEquals(args[i]); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return in, false, apperr.New("invalid_arguments", "invalid value for "+name+": expected true or false")
			}
			switch name {
			case "--stdin":
				useStdin = parsed
			case "--raw":
				in.Capture.Raw = parsed
			case "--trace":
				in.Capture.Trace = parsed
			case "--detach":
				in.Detach = parsed
			case "--keep":
				in.Keep = parsed
			default:
				// This was an equals form for a non-boolean flag. Leave it for
				// the normal parser so it gets the command-specific error.
				rest = append(rest, args[i])
			}
			continue
		}
		switch args[i] {
		case "--prompt-file", "--timeout", "--history":
			if i+1 >= len(args) {
				return in, false, apperr.New("invalid_arguments", "missing value for "+args[i])
			}
			switch args[i] {
			case "--prompt-file":
				promptFile = args[i+1]
			case "--timeout":
				timeoutRaw = args[i+1]
			case "--history":
				n, err := strconv.Atoi(args[i+1])
				if err != nil || n < -1 {
					return in, false, apperr.New("invalid_arguments", "invalid value for --history: must be -1 or a non-negative integer")
				}
				in.Capture.History = n
			}
			i++
		case "--stdin":
			useStdin = true
		case "--raw":
			in.Capture.Raw = true
		case "--trace":
			in.Capture.Trace = true
		case "--detach":
			in.Detach = true
		case "--keep":
			in.Keep = true
		default:
			rest = append(rest, args[i])
		}
	}
	summon, err := parseSummonArgs(rest)
	if err != nil {
		return in, false, err
	}
	in.Summon = summon
	if summon.Prompt != nil {
		in.Prompt = *summon.Prompt
	}
	if promptFile != "" {
		if in.Prompt != "" || useStdin {
			return in, false, apperr.New("invalid_arguments", "--prompt-file cannot be combined with --prompt or --stdin")
		}
		text, err := readPromptFile(promptFile)
		if err != nil {
			return in, false, err
		}
		in.Prompt = text
	}
	if useStdin && in.Prompt != "" {
		return in, false, apperr.New("invalid_arguments", "--stdin cannot be used with --text or --prompt")
	}
	if timeoutRaw != "" {
		ms, err := parseMillisOrDuration(timeoutRaw, "--timeout")
		if err != nil {
			return in, false, err
		}
		in.TimeoutMS = ms
	}
	if in.Detach && (in.Capture.Raw || in.Capture.Trace || in.Capture.History >= 0) {
		return in, false, apperr.New("invalid_arguments", "--detach cannot be combined with --history, --trace, or --raw; nothing is captured until you call capture or wait --collect yourself")
	}
	return in, useStdin, nil
}

// splitEqualsForms rewrites --flag=value into --flag value for the flags this
// command consumes by hand, so both spellings behave the same.
func splitEqualsForms(args []string, flags ...string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		matched := false
		for _, flag := range flags {
			if strings.HasPrefix(arg, flag+"=") {
				out = append(out, flag, strings.TrimPrefix(arg, flag+"="))
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, arg)
		}
	}
	return out
}

func splitBooleanEquals(arg string) (name, value string, ok bool) {
	for _, flag := range []string{"--stdin", "--raw", "--trace", "--detach", "--keep"} {
		if strings.HasPrefix(arg, flag+"=") {
			return flag, strings.TrimPrefix(arg, flag+"="), true
		}
	}
	return "", "", false
}

// splitNamedArgs lets the conventional positional argument and flag order
// work in either direction while keeping flag values attached to their flag.
// The standard flag package stops parsing at the first positional argument,
// so commands with an instance name need this small normalization step.
func splitNamedArgs(args []string, valueFlags map[string]bool) (positionals, flags []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		if strings.Contains(arg, "=") || !valueFlags[arg] {
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, apperr.New("invalid_arguments", "missing value for "+arg)
		}
		flags = append(flags, args[i+1])
		i++
	}
	return positionals, flags, nil
}

func parseNamedInstanceArgs(args []string, command, help string, valueFlags map[string]bool) (string, []string, error) {
	positionals, flags, err := splitNamedArgs(args, valueFlags)
	if err != nil {
		return "", nil, err
	}
	if len(positionals) == 0 {
		return "", nil, apperr.New("invalid_arguments", "missing instance name\n\n"+help)
	}
	if len(positionals) > 1 {
		return "", nil, apperr.New("invalid_arguments", command+" accepts exactly one instance name\n\n"+help)
	}
	if err := requireInstanceName(positionals[0], command, help); err != nil {
		return "", nil, err
	}
	return positionals[0], flags, nil
}

func parseListArgs(args []string) (includeEnded bool, err error) {
	fs := newFlagSet("list")
	fs.BoolVar(&includeEnded, "all", false, "")
	if err := parseFlagSet(fs, args); err != nil {
		return false, err
	}
	if fs.NArg() > 0 {
		return false, apperr.New("invalid_arguments", "list does not accept positional arguments\n\n"+listHelp())
	}
	return includeEnded, nil
}

func parsePromptArgs(args []string) (name, text, key string, useStdin bool, waitIfBusyMS int, err error) {
	name, flagArgs, err := parseNamedInstanceArgs(args, "prompt", promptHelp(), map[string]bool{
		"--text":         true,
		"--key":          true,
		"--wait-if-busy": true,
	})
	if err != nil {
		return "", "", "", false, 0, err
	}
	var waitIfBusyRaw string
	fs := newFlagSet("prompt")
	fs.StringVar(&text, "text", "", "")
	fs.StringVar(&key, "key", "", "")
	fs.BoolVar(&useStdin, "stdin", false, "")
	fs.StringVar(&waitIfBusyRaw, "wait-if-busy", "", "")
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return "", "", "", false, 0, err
	}
	if fs.NArg() > 0 {
		return "", "", "", false, 0, apperr.New("invalid_arguments", "prompt does not accept positional arguments after instance name")
	}
	if useStdin && text != "" {
		return "", "", "", false, 0, apperr.New("invalid_arguments", "--stdin cannot be used with --text")
	}
	if waitIfBusyRaw != "" {
		waitIfBusyMS, err = parseMillisOrDuration(waitIfBusyRaw, "--wait-if-busy")
		if err != nil {
			return "", "", "", false, 0, err
		}
	}
	return name, text, key, useStdin, waitIfBusyMS, nil
}

func parseCaptureArgs(args []string) (name string, opts capture.Options, err error) {
	name, flagArgs, err := parseNamedInstanceArgs(args, "capture", captureHelp(), map[string]bool{
		"--history": true,
		"--since":   true,
		"--scope":   true,
	})
	if err != nil {
		return "", capture.Options{}, err
	}
	opts = capture.Options{History: -1, Scope: capture.ScopeCurrent}
	var scopeRaw string
	fs := newFlagSet("capture")
	fs.IntVar(&opts.History, "history", -1, "")
	fs.BoolVar(&opts.Raw, "raw", false, "")
	fs.StringVar(&opts.Since, "since", "", "")
	fs.BoolVar(&opts.Trace, "trace", false, "")
	fs.BoolVar(&opts.New, "new", false, "")
	fs.StringVar(&scopeRaw, "scope", string(capture.ScopeCurrent), "")
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return "", capture.Options{}, err
	}
	if fs.NArg() > 0 {
		return "", capture.Options{}, apperr.New("invalid_arguments", "capture does not accept positional arguments after instance name")
	}
	if opts.History < -1 {
		return "", capture.Options{}, apperr.New("invalid_arguments", "invalid value for --history: must be -1 or a non-negative integer")
	}
	if opts.New && strings.TrimSpace(opts.Since) != "" {
		return "", capture.Options{}, apperr.New("invalid_arguments", "--new cannot be combined with --since")
	}
	switch capture.Scope(strings.TrimSpace(scopeRaw)) {
	case capture.ScopeCurrent, "":
		opts.Scope = capture.ScopeCurrent
	case capture.ScopeSession:
		opts.Scope = capture.ScopeSession
	default:
		return "", capture.Options{}, apperr.New("invalid_arguments", "invalid value for --scope: must be current or session")
	}
	return name, opts, nil
}

func parseLogsArgs(args []string) (name string, follow bool, err error) {
	name, flagArgs, err := parseNamedInstanceArgs(args, "logs", logsHelp(), nil)
	if err != nil {
		return "", false, err
	}
	fs := newFlagSet("logs")
	fs.BoolVar(&follow, "follow", false, "")
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return "", false, err
	}
	if fs.NArg() > 0 {
		return "", false, apperr.New("invalid_arguments", "logs does not accept positional arguments after instance name")
	}
	return name, follow, nil
}

// requireInstanceName rejects a flag-looking positional value. Named commands
// accept flags before or after the instance name, but an actual instance name
// still cannot begin with a dash.
func requireInstanceName(name, command, help string) error {
	if strings.HasPrefix(name, "-") {
		return apperr.New("invalid_arguments", command+" expects an instance name, not a flag\n\n"+help)
	}
	return nil
}

func parseWaitArgs(args []string) (names []string, stableMS, timeoutMS int, mode service.WaitMode, collect bool, err error) {
	positionals, flagArgs, splitErr := splitNamedArgs(args, map[string]bool{
		"--stable":  true,
		"--timeout": true,
		"--mode":    true,
	})
	if splitErr != nil {
		return nil, 0, 0, "", false, splitErr
	}
	for _, name := range positionals {
		if err := requireInstanceName(name, "wait", waitHelp()); err != nil {
			return nil, 0, 0, "", false, err
		}
		names = appendUnique(names, name)
	}
	if len(names) == 0 {
		return nil, 0, 0, "", false, apperr.New("invalid_arguments", "missing instance name\n\n"+waitHelp())
	}
	fs := newFlagSet("wait")
	var stableRaw, timeoutRaw, modeRaw string
	fs.StringVar(&stableRaw, "stable", "1500", "")
	fs.StringVar(&timeoutRaw, "timeout", "30s", "")
	fs.StringVar(&modeRaw, "mode", string(service.WaitAll), "")
	fs.BoolVar(&collect, "collect", false, "")
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return nil, 0, 0, "", false, err
	}
	switch service.WaitMode(strings.TrimSpace(modeRaw)) {
	case service.WaitAll, "":
		mode = service.WaitAll
	case service.WaitAny:
		mode = service.WaitAny
	default:
		return nil, 0, 0, "", false, apperr.New("invalid_arguments", "invalid value for --mode: must be all or any")
	}
	stableMS, err = parseMillisOrDuration(stableRaw, "--stable")
	if err != nil {
		return nil, 0, 0, "", false, err
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return nil, 0, 0, "", false, err
	}
	return names, stableMS, timeoutMS, mode, collect, nil
}

func appendUnique(names []string, name string) []string {
	for _, existing := range names {
		if existing == name {
			return names
		}
	}
	return append(names, name)
}

func parseHaltArgs(args []string) (name string, immediately bool, timeoutMS int, err error) {
	name, flagArgs, err := parseNamedInstanceArgs(args, "halt", haltHelp(), map[string]bool{"--timeout": true})
	if err != nil {
		return "", false, 0, err
	}
	fs := newFlagSet("halt")
	var timeoutRaw string
	fs.BoolVar(&immediately, "immediately", false, "")
	fs.StringVar(&timeoutRaw, "timeout", "5s", "")
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return "", false, 0, err
	}
	if fs.NArg() > 0 {
		return "", false, 0, apperr.New("invalid_arguments", "halt does not accept positional arguments after instance name")
	}
	if immediately && timeoutRaw != "5s" {
		return "", false, 0, apperr.New("invalid_arguments", "--timeout cannot be used with --immediately")
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return "", false, 0, err
	}
	return name, immediately, timeoutMS, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlagSet(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return apperr.New("invalid_arguments", normalizeFlagError(err.Error()))
	}
	return nil
}

func normalizeFlagError(message string) string {
	message = strings.TrimSpace(message)
	const prefix = "flag provided but not defined: -"
	if strings.HasPrefix(message, prefix) {
		return "unknown flag: --" + strings.TrimPrefix(message, prefix)
	}
	if strings.HasPrefix(message, "flag needs an argument: -") {
		return "flag needs an argument: --" + strings.TrimPrefix(message, "flag needs an argument: -")
	}
	message = strings.Replace(message, " for flag -", " for flag --", 1)
	return message
}

func parseMillisOrDuration(raw, flagName string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(value); err == nil {
		if n < 0 {
			return 0, apperr.New("invalid_arguments", "invalid value for "+flagName+": must be non-negative")
		}
		return n, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, apperr.New("invalid_arguments", "invalid value for "+flagName)
	}
	if d < 0 {
		return 0, apperr.New("invalid_arguments", "invalid value for "+flagName+": must be non-negative")
	}
	return int(d.Milliseconds()), nil
}
