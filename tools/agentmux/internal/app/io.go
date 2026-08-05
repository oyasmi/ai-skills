package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/logx"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

func attach(ctx context.Context, svc service.Service, name string, stderr io.Writer) int {
	inst, err := svc.Inspect(ctx, name)
	if err != nil {
		return writeErr(io.Discard, stderr, false, "attach", name, err)
	}
	if inst.Ended() {
		return writeErr(io.Discard, stderr, false, "attach", name,
			apperr.New("process_not_running", fmt.Sprintf("instance %q has stopped (%s); nothing to attach to", inst.Name, inst.EndReason)))
	}
	cmd := svc.AttachCommand(inst)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func attachSelect(ctx context.Context, svc service.Service, stderr io.Writer) int {
	if fi, _ := os.Stdin.Stat(); (fi.Mode() & os.ModeCharDevice) == 0 {
		return writeErr(io.Discard, stderr, false, "attach", "", apperr.New("invalid_arguments", "attach without instance requires a tty"))
	}
	items, err := svc.List(ctx, false)
	if err != nil {
		return writeErr(io.Discard, stderr, false, "attach", "", err)
	}
	if len(items) == 0 {
		fmt.Fprintln(stderr, "no instances")
		return 1
	}
	for i, item := range items {
		fmt.Fprintf(stderr, "%d. %s (%s)\n", i+1, item.Name, item.Status)
	}
	fmt.Fprint(stderr, "select instance: ")
	var choice int
	if _, err := fmt.Fscan(os.Stdin, &choice); err != nil || choice < 1 || choice > len(items) {
		fmt.Fprintln(stderr, "invalid selection")
		return 1
	}
	return attach(ctx, svc, items[choice-1].Name, stderr)
}

func extractJSON(args []string) (bool, []string, error) {
	out := make([]string, 0, len(args))
	jsonMode := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
			continue
		}
		if strings.HasPrefix(arg, "--json=") {
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json="))
			if err != nil {
				return false, nil, apperr.New("invalid_arguments", "invalid value for --json: expected true or false")
			}
			jsonMode = value
			continue
		}
		out = append(out, arg)
	}
	return jsonMode, out, nil
}

func writeErr(stdout, stderr io.Writer, jsonMode bool, command, instance string, err error) int {
	message := shortError(err)
	logx.Debug("command_error", map[string]any{
		"command":    command,
		"instance":   instance,
		"error_code": apperr.Code(err),
		"error":      message,
	})
	if jsonMode {
		_ = output.WriteJSON(stdout, output.Failure{
			OK:        false,
			Command:   command,
			Instance:  instance,
			ErrorCode: apperr.Code(err),
			Error:     message,
		})
		return 1
	}
	fmt.Fprintln(stderr, err.Error())
	return 1
}

func shortError(err error) string {
	message := strings.TrimSpace(err.Error())
	if index := strings.Index(message, "\n\n"); index >= 0 {
		message = strings.TrimSpace(message[:index])
	}
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		message = strings.TrimSpace(message[:index])
	}
	return message
}
