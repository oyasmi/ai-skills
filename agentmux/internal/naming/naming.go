package naming

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func GenerateName(templateName, cwd string) string {
	base := strings.TrimSpace(templateName)
	if base == "" {
		base = "agent"
	}
	suffix := randomHex(2)
	if cwd != "" && cwd != "." && cwd != "/" {
		parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
		last := parts[len(parts)-1]
		if last != "" && last != "." {
			return base + "-" + last + "-" + suffix
		}
	}
	return base + "-" + suffix
}

func GenerateSessionID() string {
	return "i_" + randomHex(4)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "0000"
	}
	return hex.EncodeToString(buf)
}
