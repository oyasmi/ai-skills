package providers

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/httpjson"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/jsonpath"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// Zai is the built-in z.ai (Zhipu) provider. China-mainland endpoint only,
// matching QuotaList.
type Zai struct {
	token  string
	client *httpjson.Client
}

func NewZai(cfg config.Config) *Zai {
	return &Zai{
		token:  cfg.ZaiToken,
		client: httpjson.New(cfg.Proxy, cfg.ZaiUseProxy, 8*time.Second),
	}
}

func (p *Zai) ID() string   { return "zai" }
func (p *Zai) Name() string { return "z.ai" }

func (p *Zai) Fetch(ctx context.Context) quota.Snapshot {
	if p.token == "" {
		return quota.Fail(p.ID(), p.Name(), quota.StateNoData, "未配置 z.ai token")
	}

	res := p.client.Fetch(ctx, "GET", "https://open.bigmodel.cn/api/monitor/usage/quota/limit", map[string]string{
		"authorization": "Bearer " + p.token,
		"accept":        "application/json",
	}, nil)

	switch res.Kind {
	case httpjson.KindTransport:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "网络请求失败")
	case httpjson.KindNotJSON:
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	case httpjson.KindStatus:
		if res.StatusCode == 401 {
			return quota.Fail(p.ID(), p.Name(), quota.StateError, "token 无效")
		}
		return quota.Fail(p.ID(), p.Name(), quota.StateError, fmt.Sprintf("接口返回异常 HTTP %d", res.StatusCode))
	}

	body, ok := res.Body.(map[string]any)
	if !ok {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	}
	limitsRaw, ok := data["limits"].([]any)
	if !ok {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "响应解析失败")
	}

	windows := zaiWindows(limitsRaw)
	if len(windows) == 0 {
		return quota.Fail(p.ID(), p.Name(), quota.StateError, "未识别到额度字段")
	}

	plan := ""
	for _, key := range []string{"planName", "plan", "plan_type", "packageName"} {
		if s, ok := data[key].(string); ok && s != "" {
			plan = s
			break
		}
	}

	return quota.Snapshot{
		ID: p.ID(), Name: p.Name(), Plan: plan,
		State: quota.StateOK, Windows: windows, FetchedAt: time.Now(),
	}
}

type zaiEntry struct {
	minutes int
	label   string
	percent float64
	resetAt *time.Time
}

func zaiWindows(limits []any) []quota.Window {
	var entries []zaiEntry
	for _, raw := range limits {
		limit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if limit["type"] != "TOKENS_LIMIT" {
			continue
		}
		percent, ok := jsonpath.Number(limit["percentage"])
		if !ok {
			continue
		}
		unit, _ := jsonpath.Number(limit["unit"])
		number, _ := jsonpath.Number(limit["number"])
		var resetAt *time.Time
		if ms, ok := jsonpath.Number(limit["nextResetTime"]); ok {
			t := time.UnixMilli(int64(ms))
			resetAt = &t
		}
		entries = append(entries, zaiEntry{
			minutes: zaiWindowMinutes(int(unit), int(number)),
			label:   zaiWindowLabel(int(unit), int(number)),
			percent: percent,
			resetAt: resetAt,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].minutes < entries[j].minutes })

	windows := make([]quota.Window, 0, len(entries))
	seen := map[string]int{}
	for _, e := range entries {
		label := e.label
		key := label
		if n, ok := seen[key]; ok {
			seen[key] = n + 1
			label = fmt.Sprintf("%s %d", e.label, n+1)
		} else {
			seen[key] = 1
		}
		windows = append(windows, quota.NewWindow(zaiWindowKey(label), label, e.percent, e.resetAt))
	}
	return windows
}

func zaiWindowKey(label string) string {
	return "zai_" + label
}

func zaiWindowMinutes(unit, number int) int {
	switch unit {
	case 5:
		return number
	case 3:
		return number * 60
	case 1:
		return number * 1440
	case 6:
		return number * 10080
	default:
		return 1 << 30
	}
}

func zaiWindowLabel(unit, number int) string {
	switch unit {
	case 5:
		return fmt.Sprintf("%d分钟", number)
	case 3:
		return fmt.Sprintf("%d小时", number)
	case 1:
		if number == 7 {
			return "一周"
		}
		return fmt.Sprintf("%d天", number)
	case 6:
		if number == 1 {
			return "一周"
		}
		return fmt.Sprintf("%d周", number)
	default:
		return "用量"
	}
}
