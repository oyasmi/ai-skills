package providers

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/httpjson"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/jsonpath"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// Codex reads Codex CLI's usage the same way QuotaList does: prefer the live
// ChatGPT usage API (matches the web page), falling back to the most recent
// local session file's `rate_limits` when the API is unreachable or there is
// no access token. Plan/expiry come from the `auth.json` id_token.
type Codex struct {
	client *httpjson.Client
}

func NewCodex(cfg config.Config) *Codex {
	return &Codex{client: httpjson.New(cfg.Proxy, cfg.CodexUseProxy, 15*time.Second)}
}

func (p *Codex) ID() string   { return "codex" }
func (p *Codex) Name() string { return "Codex" }

func (p *Codex) Fetch(ctx context.Context) quota.Snapshot {
	auth := codexAuthInfo()

	var windows []quota.Window
	source := ""
	if auth.accessToken != "" {
		windows = p.fetchAPIWindows(ctx, auth.accessToken, auth.accountID)
		if len(windows) > 0 {
			source = "usage_api"
		}
	}
	if len(windows) == 0 {
		if w := codexLatestWindows(); len(w) > 0 {
			windows = w
			source = "session_file"
		}
	}

	if len(windows) == 0 {
		if auth.plan == "" && auth.validUntil == nil {
			return quota.Fail(p.ID(), p.Name(), quota.StateNoData, "未找到 Codex 登录信息（先运行 `codex` 登录）")
		}
		return quota.Snapshot{
			ID: p.ID(), Name: p.Name(), Plan: auth.plan,
			State: quota.StateError, Error: "额度接口暂不可用",
			FetchedAt: time.Now(), ValidUntil: auth.validUntil,
		}
	}

	return quota.Snapshot{
		ID: p.ID(), Name: p.Name(), Plan: auth.plan,
		State: quota.StateOK, Source: source,
		Windows: windows, FetchedAt: time.Now(), ValidUntil: auth.validUntil,
	}
}

func (p *Codex) fetchAPIWindows(ctx context.Context, accessToken, accountID string) []quota.Window {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Accept":        "application/json",
		"User-Agent":    "aiquota",
	}
	if accountID != "" {
		headers["ChatGPT-Account-Id"] = accountID
	}
	res := p.client.Fetch(ctx, "GET", "https://chatgpt.com/backend-api/wham/usage", headers, nil)
	if res.Kind != httpjson.KindSuccess {
		return nil
	}
	body, ok := res.Body.(map[string]any)
	if !ok {
		return nil
	}
	rateLimit, ok := body["rate_limit"].(map[string]any)
	if !ok {
		return nil
	}
	var windows []quota.Window
	if w := codexWindowUsedPercent(rateLimit, "primary_window", "5h", "5小时", "reset_at"); w != nil {
		windows = append(windows, *w)
	}
	if w := codexWindowUsedPercent(rateLimit, "secondary_window", "7d", "一周", "reset_at"); w != nil {
		windows = append(windows, *w)
	}
	return windows
}

func codexWindowUsedPercent(m map[string]any, key, windowKey, label, resetField string) *quota.Window {
	dict, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	used, ok := jsonpath.Number(dict["used_percent"])
	if !ok {
		return nil
	}
	var resetAt *time.Time
	if t, ok := jsonpath.Date(dict[resetField]); ok {
		resetAt = &t
	}
	w := quota.NewWindow(windowKey, label, used, resetAt)
	return &w
}

// codexLatestWindows reads `token_count.rate_limits` from the newest line of
// the most recently modified session JSONL file.
func codexLatestWindows() []quota.Window {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(home, ".codex", "sessions")
	latest := latestJSONL(root)
	if latest == "" {
		return nil
	}
	text := tailFile(latest, 512*1024)
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		payload, ok := obj["payload"].(map[string]any)
		if !ok || payload["type"] != "token_count" {
			continue
		}
		rateLimits, ok := payload["rate_limits"].(map[string]any)
		if !ok {
			continue
		}
		var windows []quota.Window
		if w := codexWindowUsedPercent(rateLimits, "primary", "5h", "5小时", "resets_at"); w != nil {
			windows = append(windows, *w)
		}
		if w := codexWindowUsedPercent(rateLimits, "secondary", "7d", "一周", "resets_at"); w != nil {
			windows = append(windows, *w)
		}
		return windows
	}
	return nil
}

func latestJSONL(root string) string {
	var latestPath string
	var latestMod time.Time
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latestPath = path
		}
		return nil
	})
	return latestPath
}

// tailFile reads up to the last maxBytes of a file as text (the whole file
// when smaller), dropping a possibly-partial first line, since session logs
// grow without bound and the newest `token_count` line is always near the end.
func tailFile(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	if _, err := f.Seek(start, 0); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	if start > 0 {
		if idx := strings.IndexByte(string(data), '\n'); idx >= 0 {
			return string(data[idx+1:])
		}
	}
	return string(data)
}

type codexAuth struct {
	plan        string
	validUntil  *time.Time
	accessToken string
	accountID   string
}

func codexAuthInfo() codexAuth {
	home, err := os.UserHomeDir()
	if err != nil {
		return codexAuth{}
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return codexAuth{}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return codexAuth{}
	}

	var info codexAuth
	tokens, _ := doc["tokens"].(map[string]any)
	if s, ok := stringField(tokens, "access_token"); ok {
		info.accessToken = s
	} else if s, ok := stringField(doc, "access_token"); ok {
		info.accessToken = s
	}
	if s, ok := stringField(tokens, "account_id"); ok {
		info.accountID = s
	} else if s, ok := stringField(doc, "account_id"); ok {
		info.accountID = s
	}

	idToken, _ := stringField(tokens, "id_token")
	if idToken == "" {
		idToken, _ = stringField(doc, "id_token")
	}
	if idToken != "" {
		if claims, ok := parseJWT(idToken); ok {
			if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
				if plan, ok := auth["chatgpt_plan_type"].(string); ok && plan != "" {
					info.plan = strings.ToUpper(plan[:1]) + plan[1:]
				}
				info.validUntil = codexNonExpired(auth["chatgpt_subscription_active_until"])
			}
		}
	}
	return info
}

// codexNonExpired mirrors QuotaList: auth.json isn't refreshed on every
// renewal, so once the embedded date is in the past treat it as stale
// metadata rather than surface a misleading "expired" badge.
func codexNonExpired(v any) *time.Time {
	t, ok := jsonpath.Date(v)
	if !ok || !t.After(time.Now()) {
		return nil
	}
	return &t
}

func stringField(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	s, ok := m[key].(string)
	return s, ok && s != ""
}
