// Package harnessarg maps agentmux's harness-neutral knobs -- which model to
// run and how hard it should think -- onto the flags each harness CLI actually
// accepts.
//
// It exists because a template is a role ("a planner runs the strongest model at
// the highest effort") while the flag that expresses that role is a per-CLI
// accident: claude spells effort `--effort`, pi spells it `--thinking`, and
// codex has no flag at all and needs `-c model_reasoning_effort=`. Keeping the
// table in one place is what lets a role say `effort: high` and stay portable
// when its harness changes.
package harnessarg

import (
	"fmt"
	"strings"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

// Effort levels are agentmux's own vocabulary, weakest first. A harness whose
// vocabulary is narrower clamps into it (see the per-harness tables below)
// rather than rejecting the role, so one effort ladder stays usable across
// every harness.
const (
	EffortOff     = "off"
	EffortMinimal = "minimal"
	EffortLow     = "low"
	EffortMedium  = "medium"
	EffortHigh    = "high"
	EffortXHigh   = "xhigh"
	EffortMax     = "max"
)

// Levels returns the accepted effort levels, weakest first.
func Levels() []string {
	return []string{EffortOff, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
}

// LevelList renders Levels() for error messages and help text.
func LevelList() string {
	return strings.Join(Levels(), ", ")
}

// ValidLevel reports whether level is part of the vocabulary.
func ValidLevel(level string) bool {
	for _, l := range Levels() {
		if l == level {
			return true
		}
	}
	return false
}

// codexEffortKey is the config key codex exposes instead of a flag.
const codexEffortKey = "model_reasoning_effort"

type spec struct {
	// modelFlags are every spelling of "pin the model" this CLI accepts. A
	// command that already carries one is left alone.
	modelFlags []string
	// renderModel builds the argv that pins a model. nil means the CLI has no
	// way to choose one, so a role must not ask.
	renderModel func(model string) []string
	// effortValues translates agentmux's vocabulary into this CLI's. A missing
	// map means the CLI cannot be told how hard to think.
	effortValues map[string]string
	// renderEffort builds the argv for an already-translated effort value.
	renderEffort func(value string) []string
	// hasEffort reports whether the command (or the model pattern, for CLIs
	// that fold thinking into it) already sets the effort knob.
	hasEffort func(command, model string) bool
}

// claudeEffortValues clamps into `claude --effort`, which accepts only
// low..max: there is no "think less than low" setting to map off/minimal onto.
var claudeEffortValues = map[string]string{
	EffortOff:     "low",
	EffortMinimal: "low",
	EffortLow:     "low",
	EffortMedium:  "medium",
	EffortHigh:    "high",
	EffortXHigh:   "xhigh",
	EffortMax:     "max",
}

// codexEffortValues follow the Responses API's reasoning.effort enum, where
// "none" is what agentmux calls off.
//
// minimal clamps to low even though the enum has it: which levels a model
// accepts is per-model, and the codex models that reject "minimal" answer with
// a 400 rather than degrading. A level agentmux offers has to launch, so the
// one level with inconsistent support is not offered through. A template that
// wants it can say so directly with `-c model_reasoning_effort=minimal`, which
// suppresses injection.
var codexEffortValues = map[string]string{
	EffortOff:     "none",
	EffortMinimal: "low",
	EffortLow:     "low",
	EffortMedium:  "medium",
	EffortHigh:    "high",
	EffortXHigh:   "xhigh",
	EffortMax:     "max",
}

// piEffortValues are identical to agentmux's: `pi --thinking` defined the
// vocabulary this package borrows.
var piEffortValues = map[string]string{
	EffortOff:     "off",
	EffortMinimal: "minimal",
	EffortLow:     "low",
	EffortMedium:  "medium",
	EffortHigh:    "high",
	EffortXHigh:   "xhigh",
	EffortMax:     "max",
}

var claudeSpec = spec{
	modelFlags:   []string{"--model"},
	renderModel:  func(model string) []string { return []string{"--model", shellQuote(model)} },
	effortValues: claudeEffortValues,
	renderEffort: func(value string) []string { return []string{"--effort", value} },
	hasEffort:    func(command, _ string) bool { return hasAnyFlag(command, "--effort") },
}

var codexSpec = spec{
	modelFlags:   []string{"--model", "-m"},
	renderModel:  func(model string) []string { return []string{"--model", shellQuote(model)} },
	effortValues: codexEffortValues,
	// codex has no effort flag; the knob is a config override, which is also
	// why the value is not shell-quoted: `codex exec` validates its command
	// prefix token by token and a quoted value would not survive that scan.
	renderEffort: func(value string) []string { return []string{"-c", codexEffortKey + "=" + value} },
	hasEffort:    func(command, _ string) bool { return hasConfigKey(command, codexEffortKey) },
}

var piSpec = spec{
	modelFlags:   []string{"--model"},
	renderModel:  func(model string) []string { return []string{"--model", shellQuote(model)} },
	effortValues: piEffortValues,
	renderEffort: func(value string) []string { return []string{"--thinking", value} },
	// pi folds thinking into the model pattern as "<model>:<thinking>", so a
	// pattern that already carries one has made the choice.
	hasEffort: func(command, model string) bool {
		return hasAnyFlag(command, "--thinking") || strings.Contains(model, ":")
	},
}

var geminiSpec = spec{
	modelFlags:  []string{"--model", "-m"},
	renderModel: func(model string) []string { return []string{"--model", shellQuote(model)} },
}

// specs is keyed by harness type. It must cover service.HarnessTypes(); a
// service-level test enforces that, since a harness with no entry here cannot
// honor a role's model or effort at all.
var specs = map[string]spec{
	"claude-code":        claudeSpec,
	"claude-code-ndjson": claudeSpec,
	"codex-cli":          codexSpec,
	"codex-cli-execjson": codexSpec,
	"pi-rpc":             piSpec,
	"gemini-cli":         geminiSpec,
}

// SupportsModel reports whether a harness can be told which model to run.
func SupportsModel(harnessType string) bool {
	s, ok := specs[harnessType]
	return ok && s.renderModel != nil
}

// SupportsEffort reports whether a harness can be told how hard to think.
func SupportsEffort(harnessType string) bool {
	s, ok := specs[harnessType]
	return ok && s.effortValues != nil
}

// EffortValue translates a canonical level into the harness's own spelling. It
// is what `$EFFORT` expands to, and it is what a caller should show when
// explaining a clamp.
func EffortValue(harnessType, level string) (string, bool) {
	s, ok := specs[harnessType]
	if !ok || s.effortValues == nil {
		return "", false
	}
	value, ok := s.effortValues[level]
	return value, ok
}

// ValidateEffort reports whether a harness can honor a level, without building
// any argv. Config loading uses it to reject a role that asks for something its
// harness has no way to do.
func ValidateEffort(harnessType, level string) error {
	if strings.TrimSpace(level) == "" {
		return nil
	}
	if !ValidLevel(level) {
		return fmt.Errorf("unknown effort %q; valid levels are %s", level, LevelList())
	}
	if _, ok := specs[harnessType]; !ok {
		return fmt.Errorf("harness %q has no known effort flag; put $EFFORT in the template command to pass it yourself", harnessType)
	}
	if !SupportsEffort(harnessType) {
		return fmt.Errorf("harness %q cannot be told how hard to think; drop effort from this template", harnessType)
	}
	return nil
}

// Flags returns the extra argv a harness needs so that model and effort take
// effect, given the template's raw (unexpanded) command.
//
// A command that already makes the choice itself -- a literal `--model` flag,
// or a `$MODEL`/`$EFFORT` placeholder positioned by hand -- is left alone: a
// hand-tuned command always wins over injection, which is what makes the
// placeholders a working escape hatch for harnesses this table does not know.
func Flags(harnessType, command, model, effort string) ([]string, error) {
	var out []string
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)

	if model != "" && !strings.Contains(command, "$MODEL") {
		s, known := specs[harnessType]
		switch {
		case !known:
			return nil, apperr.New("invalid_arguments", fmt.Sprintf("harness %q has no known model flag; put $MODEL in the template command to pass it yourself", harnessType))
		case s.renderModel == nil:
			return nil, apperr.New("invalid_arguments", fmt.Sprintf("harness %q cannot be told which model to run; drop model from this template", harnessType))
		case !hasAnyFlag(command, s.modelFlags...):
			out = append(out, s.renderModel(model)...)
		}
	}

	if effort != "" && !strings.Contains(command, "$EFFORT") {
		if err := ValidateEffort(harnessType, effort); err != nil {
			return nil, apperr.New("invalid_arguments", err.Error())
		}
		s := specs[harnessType]
		if !s.hasEffort(command, model) {
			out = append(out, s.renderEffort(s.effortValues[effort])...)
		}
	}
	return out, nil
}

// hasAnyFlag reports whether the command already carries one of flags, in
// either the `--flag value` or `--flag=value` spelling.
//
// The scan is deliberately token-based rather than a real shell parse: over-
// detecting a flag only means agentmux leaves the command alone, which is the
// safe direction.
func hasAnyFlag(command string, flags ...string) bool {
	for _, f := range strings.Fields(command) {
		for _, want := range flags {
			if f == want || strings.HasPrefix(f, want+"=") {
				return true
			}
		}
	}
	return false
}

// hasConfigKey reports whether the command already overrides a codex config key
// through `-c key=value` or `--config key=value`, in either spelling.
func hasConfigKey(command, key string) bool {
	fields := strings.Fields(command)
	for i, f := range fields {
		switch {
		case f == "-c" || f == "--config":
			if i+1 < len(fields) && strings.HasPrefix(fields[i+1], key+"=") {
				return true
			}
		case strings.HasPrefix(f, "-c="), strings.HasPrefix(f, "--config="):
			if _, value, ok := strings.Cut(f, "="); ok && strings.HasPrefix(value, key+"=") {
				return true
			}
		}
	}
	return false
}

// shellQuote protects a value that reaches the harness through a shell. Model
// identifiers are user-supplied and can carry slashes, colons and dots.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
