package ndjsonctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oyasmi/agentmux/internal/instance"
)

func TestBuildClaudeCommandAddsProtocolFlags(t *testing.T) {
	cmd := buildClaudeCommand("claude --model sonnet", "be terse", "550e8400-e29b-41d4-a716-446655440000", false)
	for _, want := range []string{
		"-p",
		"--input-format stream-json",
		"--output-format stream-json",
		"--verbose",
		"--include-partial-messages",
		"--replay-user-messages",
		"--session-id '550e8400-e29b-41d4-a716-446655440000'",
		"--append-system-prompt 'be terse'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected command to contain %q, got %q", want, cmd)
		}
	}
}

func TestControllerPromptWaitCaptureAndHalt(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a local fake process")
	}
	dir := t.TempDir()
	fake := writeFakeClaude(t, dir)
	ctrl := Controller{StateDir: dir, PollMS: 10}
	inst := instance.Instance{
		Name:        "ndjson",
		Template:    "claude-code-ndjson",
		SessionID:   "i_test",
		HarnessType: HarnessType,
		CWD:         dir,
		Env:         map[string]string{},
		Status:      instance.StatusStarting,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	res, err := ctrl.Start(context.Background(), StartInput{
		Instance:        inst,
		Command:         fake,
		ClaudeSessionID: "550e8400-e29b-41d4-a716-446655440000",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	inst = res.Instance
	if inst.ProcessID <= 0 || inst.ProcessGroupID <= 0 {
		t.Fatalf("expected process metadata, got pid=%d pgid=%d", inst.ProcessID, inst.ProcessGroupID)
	}
	inst, err = ctrl.SendPrompt(context.Background(), inst, "hello")
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if inst.Status != instance.StatusBusy {
		t.Fatalf("expected busy after prompt, got %s", inst.Status)
	}
	if _, err := ctrl.Wait(context.Background(), inst, 2*time.Second); err != nil {
		t.Fatalf("wait: %v", err)
	}
	snap, err := ctrl.Capture(context.Background(), inst, 0)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if snap.Content != "done" {
		t.Fatalf("expected result content, got %q", snap.Content)
	}
	if snap.Extra["claude_session_id"] != inst.ClaudeSessionID {
		t.Fatalf("expected claude session id in extra")
	}
	if err := ctrl.Halt(context.Background(), inst, HaltOptions{Immediately: true}); err != nil {
		t.Fatalf("halt: %v", err)
	}
}

func writeFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-claude")
	script := `#!/bin/sh
printf '{"type":"system","subtype":"init","session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
while IFS= read -r line; do
  uuid=$(printf '%s' "$line" | sed -n 's/.*"uuid":"\([^"]*\)".*/\1/p')
  printf '{"type":"user","uuid":"%s","session_id":"550e8400-e29b-41d4-a716-446655440000","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}\n' "$uuid"
  printf '{"type":"assistant","session_id":"550e8400-e29b-41d4-a716-446655440000","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1}}}\n'
  printf '{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
  printf '{"type":"system","subtype":"session_state_changed","state":"idle","session_id":"550e8400-e29b-41d4-a716-446655440000"}\n'
done
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}
