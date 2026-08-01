package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/config"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/instance"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/output"
	"github.com/oyasmi/ai-skills/tools/agentmux/internal/service"
)

// doctorCheck is one diagnostic result. Status is "ok", "warn", "fail", or
// "skip"; only "fail" flips the overall doctor verdict.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// resolvedExecutablePath returns the actual file this process is running
// from, symlinks followed. It is what a caller needs to answer "which build
// am I really talking to", which os.Args[0] and PATH alone cannot answer.
func resolvedExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// runDoctor checks the environment this binary depends on: itself, PATH,
// config, state directory, registry lock, and every configured template's
// command and harness prerequisites.
//
// It does not reuse the config-loading pipeline in Run: that pipeline aborts
// on the first error, but doctor's whole purpose is to keep going and report
// every problem it can find in one pass.
func runDoctor(ctx context.Context, jsonMode bool, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return writeErr(stdout, stderr, jsonMode, "doctor", "", apperr.New("invalid_arguments", "doctor does not accept positional arguments\n\n"+doctorHelp()))
	}

	var checks []doctorCheck
	checks = append(checks, checkBinary()...)

	paths, err := config.DiscoverPaths()
	if err != nil {
		checks = append(checks, doctorCheck{"paths", "fail", err.Error()})
		return finishDoctor(checks, jsonMode, stdout)
	}
	checks = append(checks, doctorCheck{"paths", "ok", fmt.Sprintf("config=%s state=%s", paths.ConfigFile, paths.StateDir)})

	if err := config.EnsureStateDir(paths); err != nil {
		checks = append(checks, doctorCheck{"state_dir", "fail", err.Error()})
		return finishDoctor(checks, jsonMode, stdout)
	}
	checks = append(checks, checkStateDirWritable(paths)...)
	checks = append(checks, checkRegistryLock(paths)...)

	if err := config.EnsureDefaultConfig(paths.ConfigFile); err != nil {
		checks = append(checks, doctorCheck{"config_file", "fail", err.Error()})
		return finishDoctor(checks, jsonMode, stdout)
	}
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		checks = append(checks, doctorCheck{"config", "fail", err.Error()})
		return finishDoctor(checks, jsonMode, stdout)
	}
	checks = append(checks, doctorCheck{"config", "ok", fmt.Sprintf("%d templates from %s", len(cfg.Templates), paths.ConfigFile)})
	checks = append(checks, checkTemplates(cfg)...)
	checks = append(checks, checkTmux(cfg)...)

	return finishDoctor(checks, jsonMode, stdout)
}

// checkBinary reports what build is actually running and whether a plain
// `agentmux` on PATH would run the same file. This is the check that catches
// the failure mode where an old binary silently shadows a freshly installed
// one: every command still "works", just against stale behavior.
func checkBinary() []doctorCheck {
	resolved := resolvedExecutablePath()
	if resolved == "" {
		return []doctorCheck{{"binary", "warn", "cannot resolve own executable path"}}
	}

	var checks []doctorCheck
	detail := fmt.Sprintf("version=%s build_time=%s path=%s", Version, BuildTime, resolved)
	if Version == "dev" {
		checks = append(checks, doctorCheck{"binary", "warn", detail + " (unstamped build; use scripts/install.sh or scripts/release.sh so version drift stays visible)"})
	} else {
		checks = append(checks, doctorCheck{"binary", "ok", detail})
	}

	found := agentmuxesOnPath()
	switch {
	case len(found) == 0:
		checks = append(checks, doctorCheck{"path", "warn", "no `agentmux` found on PATH by name; this invocation ran " + resolved})
	case len(found) > 1:
		detail := fmt.Sprintf("multiple agentmux binaries on PATH; a plain `agentmux` runs %s. All copies: %s", found[0], strings.Join(found, ", "))
		if found[0] != resolved {
			detail += "; this invocation ran a different one: " + resolved
		}
		checks = append(checks, doctorCheck{"path", "warn", detail})
	case found[0] != resolved:
		checks = append(checks, doctorCheck{"path", "warn", fmt.Sprintf("plain `agentmux` on PATH resolves to %s, but this invocation ran %s", found[0], resolved)})
	default:
		checks = append(checks, doctorCheck{"path", "ok", "PATH resolves to the running binary: " + resolved})
	}
	return checks
}

// agentmuxesOnPath lists every distinct binary named "agentmux" reachable on
// PATH, in PATH order, symlinks resolved and de-duplicated.
func agentmuxesOnPath() []string {
	var found []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "agentmux")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		abs := candidate
		if a, err := filepath.Abs(candidate); err == nil {
			abs = a
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		found = append(found, abs)
	}
	return found
}

func checkStateDirWritable(paths config.Paths) []doctorCheck {
	f, err := os.CreateTemp(paths.StateDir, ".doctor-*")
	if err != nil {
		return []doctorCheck{{"state_dir", "fail", "cannot write to " + paths.StateDir + ": " + err.Error()}}
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return []doctorCheck{{"state_dir", "ok", paths.StateDir}}
}

func checkRegistryLock(paths config.Paths) []doctorCheck {
	if err := instance.WithLocked(paths.Registry, func(reg *instance.Registry) error { return nil }); err != nil {
		return []doctorCheck{{"registry", "fail", err.Error()}}
	}
	return []doctorCheck{{"registry", "ok", paths.Registry}}
}

// checkTemplates resolves every configured template and verifies its command's
// first word is actually runnable. This is what catches "the config points at
// `codex` but codex was never installed" before an orchestrator burns a
// summon attempt discovering it.
func checkTemplates(cfg config.Config) []doctorCheck {
	names := make([]string, 0, len(cfg.Templates))
	for name := range cfg.Templates {
		names = append(names, name)
	}
	sort.Strings(names)

	checks := make([]doctorCheck, 0, len(names))
	for _, name := range names {
		rt, err := config.Resolve(cfg, name, config.Override{})
		if err != nil {
			checks = append(checks, doctorCheck{"template:" + name, "fail", err.Error()})
			continue
		}
		fields := strings.Fields(rt.Command)
		if len(fields) == 0 {
			checks = append(checks, doctorCheck{"template:" + name, "fail", "resolved command is empty"})
			continue
		}
		bin := fields[0]
		if _, err := exec.LookPath(bin); err != nil {
			checks = append(checks, doctorCheck{"template:" + name, "fail", fmt.Sprintf("command %q not found on PATH (harness=%s)", bin, rt.HarnessType)})
			continue
		}
		checks = append(checks, doctorCheck{"template:" + name, "ok", fmt.Sprintf("%s (harness=%s)", bin, rt.HarnessType)})
	}
	return checks
}

func checkTmux(cfg config.Config) []doctorCheck {
	needsTmux := false
	for _, tpl := range cfg.Templates {
		ht := tpl.HarnessType
		if ht == "" {
			ht = cfg.Defaults.HarnessType
		}
		if !service.IsStructuredHarness(ht) {
			needsTmux = true
			break
		}
	}
	if !needsTmux {
		return []doctorCheck{{"tmux", "skip", "no TUI templates configured"}}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return []doctorCheck{{"tmux", "fail", "tmux not found on PATH but a configured template needs it"}}
	}
	return []doctorCheck{{"tmux", "ok", "socket=" + cfg.Defaults.Tmux.Socket}}
}

func finishDoctor(checks []doctorCheck, jsonMode bool, stdout io.Writer) int {
	ok := true
	for _, c := range checks {
		if c.Status == "fail" {
			ok = false
		}
	}
	if jsonMode {
		_ = output.WriteJSON(stdout, output.Success{OK: ok, Command: "doctor", Data: map[string]any{"checks": checks}})
	} else {
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tCHECK\tDETAIL")
		for _, c := range checks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", strings.ToUpper(c.Status), c.Name, c.Detail)
		}
		_ = w.Flush()
		if !ok {
			fmt.Fprintln(stdout, "\nsome checks failed; fix them before relying on agentmux")
		}
	}
	if !ok {
		return 1
	}
	return 0
}
