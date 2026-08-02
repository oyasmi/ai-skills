package providers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/httpjson"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/jsonpath"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// Custom is a config-driven HTTP provider for third-party coding plans,
// configured the same way as QuotaList's CustomProvider: explicit keypath
// mappings first, then heuristic auto-detect.
type Custom struct {
	cfg    config.CustomProvider
	client *httpjson.Client
}

func NewCustom(cfg config.CustomProvider, appCfg config.Config) *Custom {
	return &Custom{
		cfg:    cfg,
		client: httpjson.New(appCfg.Proxy, cfg.UseProxy, 8*time.Second),
	}
}

func (p *Custom) ID() string { return "custom:" + p.cfg.ID }

func (p *Custom) Name() string {
	if p.cfg.DisplayName != "" {
		return p.cfg.DisplayName
	}
	return "自定义"
}

func (p *Custom) Fetch(ctx context.Context) quota.Snapshot {
	if p.cfg.URL == "" {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "URL 无效")
	}

	headers := map[string]string{}
	for k, v := range p.cfg.Headers {
		headers[k] = v
	}
	var body []byte
	if p.cfg.Body != "" {
		body = []byte(p.cfg.Body)
		if !hasContentType(headers) {
			headers["Content-Type"] = "application/json"
		}
	}
	method := strings.ToUpper(p.cfg.Method)
	if method == "" {
		method = "GET"
	}

	res := p.client.Fetch(ctx, method, p.cfg.URL, headers, body)
	switch res.Kind {
	case httpjson.KindTransport:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "网络请求失败")
	case httpjson.KindStatus, httpjson.KindNotJSON:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "接口返回异常")
	}

	var windows []quota.Window
	for _, m := range p.cfg.Windows {
		if w := customWindow(m, res.Body); w != nil {
			windows = append(windows, *w)
		}
	}
	if len(windows) > 0 {
		return quota.Snapshot{ID: p.ID(), Name: p.Name(), State: quota.StateOK, Windows: windows, FetchedAt: time.Now()}
	}

	if p.cfg.DetectEnabled() {
		if detected, ok := jsonpath.DetectUsage(res.Body); ok {
			w := quota.NewWindow("usage", "用量", detected.UsedPercent, detected.ResetAt)
			return quota.Snapshot{ID: p.ID(), Name: p.Name(), State: quota.StateOK, Windows: []quota.Window{w}, FetchedAt: time.Now()}
		}
	}

	return quota.Fail(p.ID(), p.Name(), quota.StateError, "未识别到额度字段，可在配置文件中指定字段路径")
}

func hasContentType(headers map[string]string) bool {
	for k := range headers {
		if strings.EqualFold(k, "Content-Type") {
			return true
		}
	}
	return false
}

func customWindow(m config.WindowMapping, body any) *quota.Window {
	var resetAt *time.Time
	if m.ResetPath != "" {
		if t, ok := jsonpath.Date(jsonpath.Value(m.ResetPath, body)); ok {
			resetAt = &t
		}
	}

	// Balance-style: remaining / total.
	if m.RemainingPath != "" && m.TotalPath != "" {
		remaining, remOK := jsonpath.Number(jsonpath.Value(m.RemainingPath, body))
		total, totOK := jsonpath.Number(jsonpath.Value(m.TotalPath, body))
		if remOK && totOK && total > 0 {
			used := quota.Clamp((1 - remaining/total) * 100)
			detail := m.DetailUnit + trimNumber(remaining) + " / " + m.DetailUnit + trimNumber(total)
			w := quota.NewWindow(windowKeyFor(m.Label), m.Label, used, resetAt)
			w.Detail = detail
			return &w
		}
	}

	// Percentage-style.
	if m.UsedPath != "" {
		if used, ok := jsonpath.Number(jsonpath.Value(m.UsedPath, body)); ok {
			w := quota.NewWindow(windowKeyFor(m.Label), m.Label, used, resetAt)
			return &w
		}
	}

	return nil
}

func windowKeyFor(label string) string {
	return "custom_" + strings.ToLower(strings.ReplaceAll(label, " ", "_"))
}

func trimNumber(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}
