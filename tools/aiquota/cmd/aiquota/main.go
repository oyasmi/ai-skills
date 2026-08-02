// Command aiquota is a read-only CLI for the AI subscription quota numbers
// QuotaList (the macOS menu-bar app) shows: Claude, Codex, z.ai and any
// configured custom provider. It reads QuotaList's config file so the two
// tools share one source of truth, and never writes to it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/providers"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
	"github.com/oyasmi/ai-skills/tools/aiquota/internal/render"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("aiquota", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "输出 JSON 而不是文本表格")
	fs.BoolVar(&jsonOut, "j", false, "同 -json")

	var configPath string
	fs.StringVar(&configPath, "config", "", "配置文件路径（默认 ~/.config/quota-list/config.json，可用 AIQUOTA_CONFIG 覆盖）")
	fs.StringVar(&configPath, "c", "", "同 -config")

	var providerFilter string
	fs.StringVar(&providerFilter, "provider", "", "只查询指定渠道，逗号分隔，如 claude,codex")
	fs.StringVar(&providerFilter, "p", "", "同 -provider")

	var timeout time.Duration
	fs.DurationVar(&timeout, "timeout", 15*time.Second, "所有渠道并发请求共用的总体超时上限")
	fs.DurationVar(&timeout, "t", 15*time.Second, "同 -timeout")

	var refresh bool
	fs.BoolVar(&refresh, "refresh", false, "跳过本地节流缓存，强制向上游重新查询")
	fs.BoolVar(&refresh, "r", false, "同 -refresh")

	var noColor bool
	fs.BoolVar(&noColor, "no-color", false, "禁用文本输出的颜色")
	fs.BoolVar(&noColor, "n", false, "同 -no-color")

	var showVersion bool
	fs.BoolVar(&showVersion, "version", false, "打印版本信息")
	fs.BoolVar(&showVersion, "v", false, "同 -version")

	fs.Usage = func() { printHelp(stderr) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if showVersion {
		fmt.Fprintf(stdout, "aiquota %s (%s)\n", version, buildTime)
		return 0
	}

	cfg, err := config.Load(config.Resolve(configPath))
	if err != nil {
		fmt.Fprintln(stderr, "aiquota:", err)
		return 1
	}

	list := providers.Build(cfg)
	if providerFilter != "" {
		list = filterProviders(list, strings.Split(providerFilter, ","))
		if len(list) == 0 {
			fmt.Fprintln(stderr, "aiquota: -provider 未匹配到任何已启用渠道")
			return 2
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	snapshots := providers.FetchAll(ctx, list, refresh)
	now := time.Now()
	for i := range snapshots {
		snapshots[i].Normalize(now)
	}
	report := quota.Report{OK: true, Now: now, Providers: snapshots}

	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, "aiquota:", err)
			return 1
		}
		return 0
	}

	if len(report.Providers) == 0 {
		fmt.Fprintf(stdout, "没有已启用的渠道。编辑 %s，或先在 QuotaList 里配置。\n", cfg.Path)
		return 0
	}
	render.Text(stdout, report, render.ColorEnabled(noColor))
	return 0
}

func filterProviders(list []quota.Provider, ids []string) []quota.Provider {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	var out []quota.Provider
	for _, p := range list {
		if want[p.ID()] {
			out = append(out, p)
		}
	}
	return out
}

func printHelp(w *os.File) {
	fmt.Fprint(w, `aiquota — 查看 AI 订阅额度剩余/使用情况（Claude / Codex / z.ai / 自定义渠道）

用法:
  aiquota [flags]

渠道由配置文件的开关决定，和 QuotaList（macOS 菜单栏应用）共用同一份配置:
  ~/.config/quota-list/config.json

Flags:
  -c, -config string
        配置文件路径（默认 ~/.config/quota-list/config.json，可用 AIQUOTA_CONFIG 覆盖）
  -j, -json
        输出 JSON 而不是文本表格
  -n, -no-color
        禁用文本输出的颜色
  -p, -provider string
        只查询指定渠道，逗号分隔，如 claude,codex
  -r, -refresh
        跳过本地节流缓存，强制向上游重新查询
  -t, -timeout duration
        所有渠道并发请求共用的总体超时上限 (default 15s)
  -v, -version
        打印版本信息
`)
}
