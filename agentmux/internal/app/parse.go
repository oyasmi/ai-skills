package app

import (
	"flag"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/oyasmi/agentmux/internal/apperr"
	"github.com/oyasmi/agentmux/internal/service"
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

func parsePromptArgs(args []string) (name, text, key string, enter, useStdin bool, err error) {
	if len(args) == 0 {
		return "", "", "", false, false, apperr.New("invalid_arguments", "missing instance name\n\n"+promptHelp())
	}
	name = args[0]
	fs := newFlagSet("prompt")
	fs.StringVar(&text, "text", "", "")
	fs.StringVar(&key, "key", "", "")
	fs.BoolVar(&enter, "enter", false, "")
	fs.BoolVar(&useStdin, "stdin", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", "", "", false, false, err
	}
	if fs.NArg() > 0 {
		return "", "", "", false, false, apperr.New("invalid_arguments", "prompt does not accept positional arguments after instance name")
	}
	if useStdin && text != "" {
		return "", "", "", false, false, apperr.New("invalid_arguments", "--stdin cannot be used with --text")
	}
	return name, text, key, enter, useStdin, nil
}

func parseCaptureArgs(args []string) (name string, history int, err error) {
	if len(args) == 0 {
		return "", 0, apperr.New("invalid_arguments", "missing instance name\n\n"+captureHelp())
	}
	name = args[0]
	history = -1
	fs := newFlagSet("capture")
	fs.IntVar(&history, "history", -1, "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", 0, err
	}
	if fs.NArg() > 0 {
		return "", 0, apperr.New("invalid_arguments", "capture does not accept positional arguments after instance name")
	}
	if history < -1 {
		return "", 0, apperr.New("invalid_arguments", "invalid value for --history: must be -1 or a non-negative integer")
	}
	return name, history, nil
}

func parseWaitArgs(args []string) (name string, stableMS, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", 0, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+waitHelp())
	}
	name = args[0]
	fs := newFlagSet("wait")
	var stableRaw string
	var timeoutRaw string
	fs.StringVar(&stableRaw, "stable", "1500", "")
	fs.StringVar(&timeoutRaw, "timeout", "30s", "")
	if err := fs.Parse(args[1:]); err != nil {
		return "", 0, 0, err
	}
	if fs.NArg() > 0 {
		return "", 0, 0, apperr.New("invalid_arguments", "wait does not accept positional arguments after instance name")
	}
	stableMS, err = parseMillisOrDuration(stableRaw, "--stable")
	if err != nil {
		return "", 0, 0, err
	}
	timeoutMS, err = parseMillisOrDuration(timeoutRaw, "--timeout")
	if err != nil {
		return "", 0, 0, err
	}
	return name, stableMS, timeoutMS, nil
}

func parseHaltArgs(args []string) (name string, immediately bool, timeoutMS int, err error) {
	if len(args) == 0 {
		return "", false, 0, apperr.New("invalid_arguments", "missing instance name\n\n"+haltHelp())
	}
	name = args[0]
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
