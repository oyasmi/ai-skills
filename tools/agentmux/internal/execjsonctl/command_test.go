package execjsonctl

import (
	"strings"
	"testing"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/apperr"
)

func TestValidateCommandAcceptsPlainExecPrefix(t *testing.T) {
	for _, cmd := range []string{
		"codex exec",
		"codex exec --sandbox workspace-write --skip-git-repo-check --model gpt-5.1-codex",
		"codex exec --sandbox=workspace-write -C /tmp --add-dir /tmp/shared",
		"/usr/local/bin/codex exec -s read-only",
	} {
		if err := validateCommand(cmd); err != nil {
			t.Fatalf("expected %q to be valid, got %v", cmd, err)
		}
	}
}

func TestValidateCommandRejectsUnsupportedInput(t *testing.T) {
	cases := map[string]string{
		"missing exec":         "codex --model gpt-5.1-codex",
		"not codex":            "claude exec",
		"resume subcommand":    "codex exec resume abc",
		"review subcommand":    "codex exec review",
		"agentmux owns json":   "codex exec --json",
		"agentmux owns output": "codex exec -o out.md",
		"approval flag":        "codex exec --ask-for-approval never",
		"approval shorthand":   "codex exec -a never",
		"ephemeral":            "codex exec --ephemeral",
		"unsupported flag":     "codex exec --output-schema schema.json",
		"positional prompt":    "codex exec hello",
		"missing flag value":   "codex exec --sandbox",
		"pipe":                 "codex exec | tee log",
		"redirect":             "codex exec > out.log",
		"chain":                "codex exec && echo done",
		"substitution":         "codex exec --model $(cat model.txt)",
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateCommand(cmd)
			if err == nil {
				t.Fatalf("expected %q to be rejected", cmd)
			}
			if code := apperr.Code(err); code != "config_invalid" {
				t.Fatalf("expected config_invalid, got %s", code)
			}
		})
	}
}

// resume is a subcommand that rejects the parent's flags, so it must land after
// the user's prefix and before --json.
func TestBuildTurnCommandPlacesResumeBetweenPrefixAndJSON(t *testing.T) {
	prefix := "codex exec --sandbox workspace-write"

	first := buildTurnCommand(prefix, "")
	if first != prefix+" --json -" {
		t.Fatalf("unexpected first-turn command: %q", first)
	}

	resumed := buildTurnCommand(prefix, "019f4659-1a62-70d0-b77d-dc2bf1464648")
	want := prefix + " resume '019f4659-1a62-70d0-b77d-dc2bf1464648' --json -"
	if resumed != want {
		t.Fatalf("unexpected resume command:\n got %q\nwant %q", resumed, want)
	}
	if strings.Index(resumed, "resume") > strings.Index(resumed, "--json") {
		t.Fatalf("resume must precede --json, got %q", resumed)
	}
	if strings.Index(resumed, "--sandbox") > strings.Index(resumed, "resume") {
		t.Fatalf("parent flags must precede resume, got %q", resumed)
	}
}
