package harnessarg

import (
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func TestFlagsRendersEachHarnessOwnSpelling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		harnessType string
		command     string
		model       string
		effort      string
		want        []string
	}{
		{
			name:        "claude",
			harnessType: "claude-code-ndjson",
			command:     "claude --dangerously-skip-permissions",
			model:       "opus",
			effort:      "max",
			want:        []string{"--model", "'opus'", "--effort", "max"},
		},
		{
			name:        "codex uses a config override for effort",
			harnessType: "codex-cli-execjson",
			command:     "codex exec --sandbox read-only",
			model:       "gpt-5.6-luna",
			effort:      "high",
			want:        []string{"--model", "'gpt-5.6-luna'", "-c", "model_reasoning_effort=high"},
		},
		{
			name:        "pi calls effort thinking",
			harnessType: "pi-rpc",
			command:     "pi",
			model:       "zai-coding-cn/glm-5.2",
			effort:      "off",
			want:        []string{"--model", "'zai-coding-cn/glm-5.2'", "--thinking", "off"},
		},
		{
			name:        "gemini takes a model and nothing else",
			harnessType: "gemini-cli",
			command:     "gemini",
			model:       "gemini-3-pro",
			want:        []string{"--model", "'gemini-3-pro'"},
		},
		{
			name:        "nothing to add",
			harnessType: "claude-code",
			command:     "claude",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Flags(tc.harnessType, tc.command, tc.model, tc.effort)
			if err != nil {
				t.Fatalf("Flags: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("flags mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// Injection must never fight a choice the template already made, in any
// spelling: doubling a flag is at best ignored and at worst an error from the
// harness, and either way it overrides a deliberate setting.
func TestFlagsLeavesAnExplicitChoiceAlone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		harnessType string
		command     string
		model       string
		effort      string
	}{
		{"long model flag", "claude-code-ndjson", "claude --model haiku --effort low", "opus", "max"},
		{"equals spelling", "claude-code-ndjson", "claude --model=haiku --effort=low", "opus", "max"},
		{"codex short model flag", "codex-cli-execjson", "codex exec -m gpt-5.5 -c model_reasoning_effort=low", "gpt-5.6-luna", "max"},
		{"codex config equals spelling", "codex-cli-execjson", "codex exec --config=model_reasoning_effort=low --model x", "y", "max"},
		{"placeholders", "pi-rpc", "pi --model $MODEL --thinking $EFFORT", "glm-5.2", "max"},
		{"pi thinking inside the model pattern", "pi-rpc", "pi --model 'glm-5.2:high'", "glm-5.2:high", "max"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Flags(tc.harnessType, tc.command, tc.model, tc.effort)
			if err != nil {
				t.Fatalf("Flags: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("expected no injection, got %v", got)
			}
		})
	}
}

// The vocabulary is agentmux's, not any one CLI's, so a level a harness lacks
// clamps into its nearest available setting rather than failing the role.
func TestEffortClampsIntoANarrowerVocabulary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		harnessType string
		level       string
		want        string
	}{
		{"claude-code", "off", "low"},
		{"claude-code", "minimal", "low"},
		{"claude-code", "low", "low"},
		{"claude-code", "max", "max"},
		{"codex-cli", "off", "none"},
		// codex has a "minimal" of its own, but not every codex model accepts
		// it, and the ones that do not answer with a 400.
		{"codex-cli", "minimal", "low"},
		{"codex-cli", "max", "max"},
		{"pi-rpc", "off", "off"},
		{"pi-rpc", "minimal", "minimal"},
	}

	for _, tc := range cases {
		got, ok := EffortValue(tc.harnessType, tc.level)
		if !ok {
			t.Fatalf("%s has no value for %q", tc.harnessType, tc.level)
		}
		if got != tc.want {
			t.Fatalf("%s effort %q: got %q, want %q", tc.harnessType, tc.level, got, tc.want)
		}
	}
}

func TestFlagsRejectsWhatItCannotHonor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		harnessType string
		command     string
		model       string
		effort      string
		wantIn      string
	}{
		{
			name:        "unknown harness cannot place a model flag",
			harnessType: "some-agent",
			command:     "some-agent --run",
			model:       "whatever",
			wantIn:      "$MODEL",
		},
		{
			name:        "unknown harness cannot place an effort flag",
			harnessType: "some-agent",
			command:     "some-agent --run",
			effort:      "high",
			wantIn:      "$EFFORT",
		},
		{
			name:        "gemini has no thinking knob",
			harnessType: "gemini-cli",
			command:     "gemini",
			effort:      "high",
			wantIn:      "cannot be told how hard to think",
		},
		{
			name:        "a misspelled level is not silently defaulted",
			harnessType: "claude-code",
			command:     "claude",
			effort:      "ultra",
			wantIn:      "unknown effort",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Flags(tc.harnessType, tc.command, tc.model, tc.effort)
			if err == nil {
				t.Fatal("expected an error")
			}
			if code := apperr.Code(err); code != "invalid_arguments" {
				t.Fatalf("expected invalid_arguments, got %s", code)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("error must mention %q, got: %v", tc.wantIn, err)
			}
		})
	}
}

// A placeholder is the documented escape hatch for a harness this table does not
// know, so it must work even where injection refuses to guess.
func TestPlaceholdersAreTheEscapeHatchForAnUnknownHarness(t *testing.T) {
	t.Parallel()

	got, err := Flags("some-agent", "some-agent --run --model $MODEL --level $EFFORT", "x", "high")
	if err != nil {
		t.Fatalf("Flags: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no injection, got %v", got)
	}
}

func TestModelIsQuotedForTheShell(t *testing.T) {
	t.Parallel()

	got, err := Flags("pi-rpc", "pi", "provider/model-1.2", "")
	if err != nil {
		t.Fatalf("Flags: %v", err)
	}
	if len(got) != 2 || got[1] != "'provider/model-1.2'" {
		t.Fatalf("model must reach the shell quoted, got %v", got)
	}
}

func TestValidLevelCoversTheDocumentedVocabulary(t *testing.T) {
	t.Parallel()

	for _, level := range Levels() {
		if !ValidLevel(level) {
			t.Errorf("Levels() lists %q but ValidLevel rejects it", level)
		}
	}
	if ValidLevel("") || ValidLevel("HIGH") || ValidLevel("ultra") {
		t.Error("ValidLevel must not accept an empty, differently-cased, or invented level")
	}
	if want := "off, minimal, low, medium, high, xhigh, max"; LevelList() != want {
		t.Errorf("LevelList() = %q, want %q", LevelList(), want)
	}
}
