package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/httpjson"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/jsonpath"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// Claude reads Claude Code's local OAuth credential and calls the same usage
// API the CLI itself uses. Unlike QuotaList there is no Web-cookie path here:
// that requires a browser session key the user pastes into a GUI, which has
// no CLI equivalent, so only the OAuth path (always available once `claude`
// has been logged in) is implemented.
//
// This is strictly read-only: it never calls the credential file's
// refreshToken to mint a new accessToken (that would mean reimplementing an
// undocumented OAuth flow). An expired accessToken is reported as an error;
// running the `claude` CLI once refreshes it on disk the normal way.
type Claude struct {
	client *httpjson.Client
}

func NewClaude(cfg config.Config) *Claude {
	return &Claude{client: httpjson.New(cfg.Proxy, cfg.ClaudeUseProxy, 15*time.Second)}
}

func (p *Claude) ID() string   { return "claude" }
func (p *Claude) Name() string { return "Claude" }

func (p *Claude) Fetch(ctx context.Context) quota.Snapshot {
	cred, err := claudeCredential()
	if err != nil {
		return quota.Fail(p.ID(), p.Name(), quota.StateNoData, "未找到 Claude Code 登录凭据（先运行 `claude` 登录）")
	}
	if time.Now().Unix() >= cred.expiresAt-60 {
		// The stored access token is past its expiry, but `claude` refreshes it
		// automatically on next use (using the credential file's refreshToken,
		// which this tool never calls itself — see package doc). Most of the
		// time this just means the CLI hasn't been run in the last hour, not
		// that the user is actually logged out.
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "Claude Code 登录凭据已过期，运行一次 `claude` 命令即可自动刷新；仍不行再重新登录")
	}

	res := p.client.Fetch(ctx, "GET", "https://api.anthropic.com/api/oauth/usage", map[string]string{
		"Authorization":  "Bearer " + cred.token,
		"Accept":         "application/json",
		"Content-Type":   "application/json",
		"anthropic-beta": "oauth-2025-04-20",
		"User-Agent":     "aiquota",
	}, nil)

	switch res.Kind {
	case httpjson.KindTransport:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "网络请求失败")
	case httpjson.KindNotJSON:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	case httpjson.KindStatus:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, claudeStatusMessage(res))
	}

	body, ok := res.Body.(map[string]any)
	if !ok {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	}

	var windows []quota.Window
	if w := claudeWindow(body, "five_hour", "5h", "5小时"); w != nil {
		windows = append(windows, *w)
	}
	if w := claudeWindow(body, "seven_day", "7d", "一周"); w != nil {
		windows = append(windows, *w)
	}
	if w := claudeWindow(body, "seven_day_sonnet", "7d_sonnet", "Sonnet"); w != nil {
		windows = append(windows, *w)
	}
	if w := claudeWindow(body, "seven_day_opus", "7d_opus", "Opus"); w != nil {
		windows = append(windows, *w)
	}
	if len(windows) == 0 {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "未识别到 Claude 额度字段")
	}

	snap := quota.Snapshot{
		ID: p.ID(), Name: p.Name(), Plan: "Claude Code",
		State: quota.StateOK, Source: "oauth_usage_api",
		Windows: windows, FetchedAt: time.Now(),
		ValidUntil: claudeValidUntil(body),
	}
	return snap
}

func claudeStatusMessage(res httpjson.Result) string {
	switch res.StatusCode {
	case 401, 403:
		return "Claude 登录已失效，请重新登录"
	case 429:
		if res.RetryAfter != nil {
			return fmt.Sprintf("Claude 请求过于频繁，%s 后再试", res.RetryAfter.Format(time.RFC3339))
		}
		return "Claude 请求过于频繁，稍后再试"
	default:
		return fmt.Sprintf("Claude 接口异常 HTTP %d", res.StatusCode)
	}
}

func claudeWindow(body map[string]any, key, windowKey, label string) *quota.Window {
	dict, ok := body[key].(map[string]any)
	if !ok {
		return nil
	}
	util, ok := jsonpath.Number(dict["utilization"])
	if !ok {
		util, ok = jsonpath.Number(dict["percent"])
	}
	if !ok {
		return nil
	}
	var resetAt *time.Time
	if t, ok := jsonpath.Date(dict["resets_at"]); ok {
		resetAt = &t
	}
	w := quota.NewWindow(windowKey, label, util, resetAt)
	return &w
}

// claudeValidUntil scans the usage payload for any subscription-cycle date.
// Claude's usage endpoint is not public API and has used several names for
// this over time, so match broadly rather than pick one.
var claudeValidUntilKeys = map[string]bool{
	"subscription_active_until": true, "subscriptionActiveUntil": true,
	"current_period_end": true, "currentPeriodEnd": true,
	"next_billing_date": true, "nextBillingDate": true,
	"renewal_date": true, "renewalDate": true,
	"expires_at": true, "expiresAt": true,
	"expiration": true, "expires": true,
	"active_until": true, "activeUntil": true,
	"valid_until": true, "validUntil": true,
}

// claudeValidUntil walks the payload breadth-first so a shallow match always
// wins over a deeper one anywhere else in the tree, rather than depending on
// which sibling subtree happens to be visited first.
func claudeValidUntil(v any) *time.Time {
	queue := []any{v}
	for len(queue) > 0 {
		var next []any
		for _, node := range queue {
			switch t := node.(type) {
			case map[string]any:
				for k, val := range t {
					if claudeValidUntilKeys[k] {
						if d, ok := jsonpath.Date(val); ok {
							return &d
						}
					}
				}
				for _, val := range t {
					next = append(next, val)
				}
			case []any:
				next = append(next, t...)
			}
		}
		queue = next
	}
	return nil
}

type claudeCred struct {
	token     string
	expiresAt int64 // unix seconds
}

// claudeCredential reads ~/.claude/.credentials.json, the same file the
// `claude` CLI itself writes and reads on Linux/macOS. QuotaList additionally
// falls back to the macOS Keychain, which has no Linux equivalent and no
// meaning for a CLI running non-interactively, so it is not replicated here.
func claudeCredential() (claudeCred, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeCred{}, err
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return claudeCred{}, err
	}
	var doc struct {
		ClaudeAiOauth struct {
			AccessToken string  `json:"accessToken"`
			ExpiresAt   float64 `json:"expiresAt"` // ms since epoch
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return claudeCred{}, err
	}
	if doc.ClaudeAiOauth.AccessToken == "" {
		return claudeCred{}, fmt.Errorf("no access token in %s", path)
	}
	return claudeCred{
		token:     doc.ClaudeAiOauth.AccessToken,
		expiresAt: int64(doc.ClaudeAiOauth.ExpiresAt / 1000),
	}, nil
}
