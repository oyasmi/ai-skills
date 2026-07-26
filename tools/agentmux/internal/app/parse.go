package app

import (
	"flag"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/capture"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

func parseSummonArgs(args []string) (service.SummonInput, error) {
	in := service.SummonInput{}
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
			return in, apperr.New("invalid_arguments", "unknown flag "+args[i])
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

func parseListArgs(args []string) (includeEnded bool, err error) {
	fs := newFlagSet("list")
	fs.BoolVar(&includeEnded, "all", false, "")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if fs.NArg() > 0 {
		return false, apperr.New("invalid_arguments", "list does not accept positional arguments\n\n"+listHelp())
	}
	return includeEnded, nil
}

func parsePromptArgs(args []string) (name, text, key string, useStdin bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, apperr.New("invalid_arguments", "missing instance name\n\n"+promptHelp())
	}
	name = args[0]
	if err := requireInstanceName(name, "prompt", promptHelp()); err != nil {
		return "", "", "", false, err
	}
	fs := newFlagSet("prompt")
	fs.StringVar(&text, "text", "", "")
	fs.StringVar(&key, "key", "", "")
	fs.BoolVar(&useStdin, "stdin", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", "", "", false, err
	}
	if fs.NArg() > 0 {
		return "", "", "", false, apperr.New("invalid_arguments", "prompt does not accept positional arguments after instance name")
	}
	if useStdin && text != "" {
		return "", "", "", false, apperr.New("invalid_arguments", "--stdin cannot be used with --text")
	}
	return name, text, key, useStdin, nil
}

func parseCaptureArgs(args []string) (name string, opts capture.Options, err error) {
	if len(args) == 0 {
		return "", capture.Options{}, apperr.New("invalid_arguments", "missing instance name\n\n"+captureHelp())
	}
	name = args[0]
	if err := requireInstanceName(name, "capture", captureHelp()); err != nil {
		return "", capture.Options{}, err
	}
	opts = capture.Options{History: -1, Scope: capture.ScopeCurrent}
	var scopeRaw string
	fs := newFlagSet("capture")
	fs.IntVar(&opts.History, "history", -1, "")
	fs.BoolVar(&opts.Raw, "raw", false, "")
	fs.StringVar(&opts.Since, "since", "", "")
	fs.StringVar(&scopeRaw, "scope", string(capture.ScopeCurrent), "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", capture.Options{}, err
	}
	if fs.NArg() > 0 {
		return "", capture.Options{}, apperr.New("invalid_arguments", "capture does not accept positional arguments after instance name")
	}
	if opts.History < -1 {
		return "", capture.Options{}, apperr.New("invalid_arguments", "invalid value for --history: must be -1 or a non-negative integer")
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

// requireInstanceName rejects a flag in the instance-name position. Without it
// `agentmux capture --history 40 worker` silently treats "--history" as the
// instance and fails with an error that points nowhere near the real mistake.
func requireInstanceName(name, command, help string) error {
	if strings.HasPrefix(name, "-") {
		return apperr.New("invalid_arguments", "the instance name must come before flags: "+command+" <instance-name> [flags]\n\n"+help)
	}
	return nil
}

func parseWaitArgs(args []string) (names []string, stableMS, timeoutMS int, mode service.WaitMode, err error) {
	if len(args) == 0 {
		return nil, 0, 0, "", apperr.New("invalid_arguments", "missing instance name\n\n"+waitHelp())
	}
	if err := requireInstanceName(args[0], "wait", waitHelp()); err != nil {
		return nil, 0, 0, "", err
	}
	rest := args
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		names = appendUnique(names, rest[0])
		rest = rest[1:]
	}
	fs := newFlagSet("wait")
	var stableRaw, timeoutRaw, modeRaw string
	fs.StringVar(&stableRaw, "stable", "1500", "")
	fs.StringVar(&timeoutRaw, "timeout", "30s", "")
	fs.StringVar(&modeRaw, "mode", string(service.WaitAll), "")
	if err := fs.Parse(rest); err != nil {
		return nil, 0, 0, "", err
	}
	if fs.NArg() > 0 {
		return nil, 0, 0, "", apperr.New("invalid_arguments", "wait does not accept positional arguments after flags; list every instance name first")
	}
	switch service.WaitMode(strings.TrimSpace(modeRaw)) {
	case service.WaitAll, "":
		mode = service.WaitAll
	case service.WaitAny:
		mode = service.WaitAny
	default:
		return nil, 0, 0, "", apperr.New("invalid_arguments", "invalid value for --mode: must be all or any")
	}
	stableMS, err = parseMillisOrDuration(stableRaw, "--stable")
	if err != nil {
		return nil, 0, 0, "", err
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return nil, 0, 0, "", err
	}
	return names, stableMS, timeoutMS, mode, nil
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
	if len(args) == 0 {
		return "", false, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+haltHelp())
	}
	name = args[0]
	if err := requireInstanceName(name, "halt", haltHelp()); err != nil {
		return "", false, 0, err
	}
	fs := newFlagSet("halt")
	var timeoutRaw string
	fs.BoolVar(&immediately, "immediately", false, "")
	fs.StringVar(&timeoutRaw, "timeout", "5s", "")
	if err := fs.Parse(args[1:]); err != nil {
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
