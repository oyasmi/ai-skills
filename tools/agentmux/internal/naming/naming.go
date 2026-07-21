package naming

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/oyasmi/agentmux/internal/apperr"
)

func GenerateName(templateName, cwd string) (string, error) {
	base := strings.TrimSpace(templateName)
	if base == "" {
		base = "agent"
	}
	suffix, err := randomHex(2)
	if err != nil {
		return "", err
	}
	if cwd != "" && cwd != "." && cwd != "/" {
		parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
		last := parts[len(parts)-1]
		if last != "" && last != "." {
			return base + "-" + last + "-" + suffix, nil
		}
	}
	return base + "-" + suffix, nil
}

func GenerateSessionID() (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return "i_" + suffix, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap("internal_error", err, "generate random identifier")
	}
	return hex.EncodeToString(buf), nil
}
