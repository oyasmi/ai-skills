package naming

import (
	"strings"
	"testing"
)

func TestGenerateNameUsesTemplateAndCWDBase(t *testing.T) {
	name, err := GenerateName("worker", "/tmp/project-a")
	if err != nil {
		t.Fatalf("GenerateName: %v", err)
	}
	if !strings.HasPrefix(name, "worker-project-a-") {
		t.Fatalf("unexpected generated name: %q", name)
	}
}

func TestGenerateNameFallsBackToAgent(t *testing.T) {
	name, err := GenerateName("", ".")
	if err != nil {
		t.Fatalf("GenerateName: %v", err)
	}
	if !strings.HasPrefix(name, "agent-") {
		t.Fatalf("unexpected generated name: %q", name)
	}
}

func TestGenerateSessionIDHasPrefix(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID: %v", err)
	}
	if !strings.HasPrefix(id, "i_") {
		t.Fatalf("unexpected session id: %q", id)
	}
	if len(id) != 10 {
		t.Fatalf("unexpected session id length: %q", id)
	}
}
