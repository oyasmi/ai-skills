Plan: harness_type 驱动的 Agent 状态检测

 问题

 agentmux 管理运行在 tmux session 中的 AI coding agent。当前状态检测有两个痛点：

 1. busy→idle 不精确：依赖 busy_ttl_ms（默认30s）定时衰减，要么判定太早（agent 还在工作），要么太晚（用户空等）。
 2. wait 命令效率低：依赖屏幕内容 SHA256 稳定性轮询，对于长时间运行的 agent 任务，需要反复截屏比较。

 调研发现

 通过在 tmux 中实测，我们发现不同 agent 的 pane_title（程序通过 OSC 0/2 转义序列设置，tmux 存储为内部变量，不受 tmux 主题/配置影响，与 tab
 显示的 window_name 完全独立）行为差异很大：

 ┌─────────────┬───────────────────────────────────────────────────────────────────┬────────────┐
 │    Agent    │                          pane_title 行为                          │  可利用性  │
 ├─────────────┼───────────────────────────────────────────────────────────────────┼────────────┤
 │ Claude Code │ idle 时首字符为 ✳(U+2733)，busy 时为 Braille spinner(U+2800-28FF) │ 精确、即时 │
 ├─────────────┼───────────────────────────────────────────────────────────────────┼────────────┤
 │ Codex CLI   │ 不修改 pane_title                                                 │ 无法利用   │
 ├─────────────┼───────────────────────────────────────────────────────────────────┼────────────┤
 │ OpenCode    │ 设置任务描述，但无状态指示符                                      │ 无法利用   │
 └─────────────┴───────────────────────────────────────────────────────────────────┴────────────┘

 设计决策

 - 不使用 idle_title_patterns 让用户手填 pattern，而是引入 harness_type 字段，由 agentmux 内置各 agent 的检测逻辑。
 - harness_type: claude-code 启用 pane_title 状态检测（精确路径）。
 - 其他值（codex-cli、opencode、未知值）走已有的内容稳定性 + TTL 路径，不报错。
 - 为 claude-code 路径优化轮询开销：PaneTitle 查询单独轻量化，不捆绑完整 PaneInfo。

 Implementation

 Step 1: tmuxctl — 新增轻量 PaneTitle 查询

 File: internal/tmuxctl/tmux.go

 新增方法，只查询 #{pane_title}，比完整 PaneInfo（6个字段 + 解析）更轻量：

 func (c Client) PaneTitle(ctx context.Context, target string) (string, error) {
     return c.Display(ctx, target, "#{pane_title}")
 }

 同时在 PaneInfo 中也追加 PaneTitle string 字段（顺便带上，无额外开销），format 串末尾加 |#{pane_title}，解析用 SplitN(out, "|", 7) 防 title
 含 |。

 Step 2: config — Template 和 Defaults 增加 harness_type

 File: internal/config/config.go

 type Defaults struct {
     // ... existing ...
     HarnessType string `yaml:"harness_type"` // NEW
 }

 type Template struct {
     // ... existing ...
     HarnessType string `yaml:"harness_type"` // NEW
 }
  type ResolvedTemplate struct {
     // ... existing ...
     HarnessType string // NEW
 }

 Resolve() 中：template 有值用 template 的，否则继承 defaults 的。不做合法性校验——未知值视为通用处理。

 默认配置不设 harness_type（空字符串 = 通用模式）。

 配置示例：
 templates:
   claude-worker:
     command: claude --model $MODEL
     harness_type: claude-code      # 启用 title 检测
   codex-worker:
     command: codex --model $MODEL
     harness_type: codex-cli        # 通用模式
   opencode-worker:
     command: opencode
     # 无 harness_type → 通用模式

 Step 3: instance — Instance 增加字段

 File: internal/instance/registry.go

 type Instance struct {
     // ... existing ...
     HarnessType string `json:"harness_type,omitempty"` // NEW
     PaneTitle   string `json:"pane_title,omitempty"`   // NEW: 最近一次观测值
 }

 向后兼容：旧 registry 文件反序列化时这两个字段为零值。

 Step 4: service — 核心 reconcile 改造

 File: internal/service/service.go

 新增内置检测函数：

 // claude-code 专用：✳ (U+2733) 开头 = idle
 func claudeCodeTitleIsIdle(paneTitle string) bool {
     return len(paneTitle) > 0 && []rune(paneTitle)[0] == '\u2733'
 }

 Summon() 改造：创建 Instance 时从 resolved template 存储 HarnessType。

 reconcile() 改造：

 func (s Service) reconcile(ctx, inst) instance.Instance {
     if !HasSession → lost

     info, err := PaneInfo(target)  // 现在包含 PaneTitle
     if err → return inst

     inst.PaneTitle = info.PaneTitle  // 始终更新观测值

     if info.Dead → exited
     else if inst.Status == StatusBusy:
         if inst.HarnessType == "claude-code" && claudeCodeTitleIsIdle(info.PaneTitle):
             → idle                         // 精确路径
         elif busyExpired(inst):
             → idle                         // TTL 兜底
     else if inst.Status != StatusBusy:
         → idle

     inst.UpdatedAt = now
     return inst
 }

 Step 5: capture — WaitStable 支持 title 早退

 File: internal/capture/capture.go

 Snapshot 增加 PaneTitle string，Once() 从 PaneInfo 填充。

 新增类型和 WaitStable 签名变更：

 type TitleIdleFunc func(paneTitle string) bool

 func WaitStable(ctx, tmux, target, history, stableMS, timeoutMS, pollMS int, titleIdle TitleIdleFunc) (Snapshot, error)

 轮询循环中，每次 Once() 后检查：
 if titleIdle != nil && titleIdle(snap.PaneTitle) {
     return snap, nil  // title 说 idle 了，立即返回
 }

 调用侧（service.captureLike）：

 var titleIdle capture.TitleIdleFunc
 if inst.HarnessType == "claude-code" {
     titleIdle = func(title string) bool {
         return claudeCodeTitleIsIdle(title)
     }
 }
 snap, err := capture.WaitStable(ctx, s.Tmux, ..., titleIdle)

 对于非 claude-code agent，titleIdle 为 nil，WaitStable 行为与当前完全一致。

 Step 6: app — JSON 输出暴露新字段

 File: internal/app/app.go

 - capture / wait 命令的 JSON data 增加 "pane_title": snap.PaneTitle
 - inspect 自动继承（Data 就是 Instance）
 - list JSON 自动继承

 Step 7: 测试

 service_test.go：
 - fakeTmux.paneInfo 已含 PaneTitle 字段，设置即可
 - 新增：busy + claude-code + title ✳ → idle
 - 新增：busy + claude-code + title spinner → stays busy
 - 新增：busy + unknown harness + title ✳ → stays busy (TTL 未过期)
 - 新增：busy + empty harness → TTL 兜底

 config_test.go：
 - 新增：harness_type 在 template/defaults 中的解析和继承

 轮询开销优化说明

 对于 claude-code 路径，reconcile() 中 PaneInfo 查询只是一次 tmux display-message 调用（追加 #{pane_title} 到已有 format
 串中），零额外进程开销。

 WaitStable 轮询循环中，每次 Once() 已经调用 PaneInfo，title 检查是纯内存字符串比较。如果 title 判定 idle，立即返回，省去后续所有 content
 stability 轮询轮次。这是最大的性能收益：从等待 N 秒内容稳定 → 首次检测到 title 变化即返回。

 Verification

 1. go build ./... 编译通过
 2. go test ./... 全部通过
 3. 手动验证：
   - 配置 harness_type: claude-code 的 template
   - summon + prompt 发送任务
   - agentmux wait <name> --json 观察是否在 title 变为 ✳ 时立即返回
   - agentmux inspect <name> --json 确认 pane_title 和 harness_type 字段

 Critical Files

 - internal/tmuxctl/tmux.go — PaneTitle 查询 + PaneInfo 扩展
 - internal/service/service.go — reconcile + claudeCodeTitleIsIdle + captureLike
 - internal/config/config.go — harness_type 配置
 - internal/capture/capture.go — WaitStable title 早退
 - internal/instance/registry.go — Instance 字段扩展
 - internal/app/app.go — JSON 输出
