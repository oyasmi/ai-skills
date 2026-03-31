package tmuxctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/oyasmi/agentmux/internal/apperr"
)

type Client struct {
	Socket         string
	LoadUserConfig bool
}

func (c Client) baseArgs() []string {
	args := []string{"-S", c.Socket}
	if !c.LoadUserConfig {
		args = append([]string{"-f", "/dev/null"}, args...)
	}
	return args
}

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", append(c.baseArgs(), args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", apperr.Wrap("tmux_unavailable", err, "tmux %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func (c Client) HasSession(ctx context.Context, sessionID string) bool {
	_, err := c.run(ctx, "has-session", "-t", sessionID)
	return err == nil
}

func (c Client) NewSession(ctx context.Context, sessionID, cwd, command string, env map[string]string) error {
	args := []string{"new-session", "-d", "-s", sessionID, "-c", cwd}
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, command)
	_, err := c.run(ctx, args...)
	return err
}

func (c Client) KillSession(ctx context.Context, sessionID string) error {
	_, err := c.run(ctx, "kill-session", "-t", sessionID)
	return err
}

func (c Client) CapturePane(ctx context.Context, target string, history int) (string, error) {
	args := []string{"capture-pane", "-p", "-J", "-t", target}
	if history > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(history))
	}
	return c.run(ctx, args...)
}

func (c Client) Display(ctx context.Context, target, format string) (string, error) {
	return c.run(ctx, "display-message", "-p", "-t", target, format)
}

func (c Client) LoadBuffer(ctx context.Context, data string) error {
	cmd := exec.CommandContext(ctx, "tmux", append(c.baseArgs(), "load-buffer", "-")...)
	cmd.Stdin = strings.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return apperr.Wrap("tmux_unavailable", err, "tmux load-buffer: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (c Client) PasteBuffer(ctx context.Context, target string) error {
	_, err := c.run(ctx, "paste-buffer", "-d", "-t", target)
	return err
}

func (c Client) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := []string{"send-keys", "-t", target}
	args = append(args, keys...)
	_, err := c.run(ctx, args...)
	return err
}

func (c Client) Attach(sessionID string) *exec.Cmd {
	return exec.Command("tmux", append(c.baseArgs(), "attach-session", "-t", sessionID)...)
}

type PaneInfo struct {
	CursorX   int
	CursorY   int
	Width     int
	Height    int
	Dead      bool
	Command   string
	PaneTitle string
}

func (c Client) PaneTitle(ctx context.Context, target string) (string, error) {
	return c.Display(ctx, target, "#{pane_title}")
}

func (c Client) PaneInfo(ctx context.Context, target string) (PaneInfo, error) {
	out, err := c.Display(ctx, target, "#{cursor_x}|#{cursor_y}|#{pane_width}|#{pane_height}|#{pane_dead}|#{pane_current_command}|#{pane_title}")
	if err != nil {
		return PaneInfo{}, err
	}
	parts := strings.SplitN(out, "|", 7)
	if len(parts) != 7 {
		return PaneInfo{}, apperr.New("tmux_unavailable", "unexpected pane info format")
	}
	x, _ := strconv.Atoi(parts[0])
	y, _ := strconv.Atoi(parts[1])
	w, _ := strconv.Atoi(parts[2])
	h, _ := strconv.Atoi(parts[3])
	return PaneInfo{
		CursorX:   x,
		CursorY:   y,
		Width:     w,
		Height:    h,
		Dead:      parts[4] == "1",
		Command:   parts[5],
		PaneTitle: parts[6],
	}, nil
}
