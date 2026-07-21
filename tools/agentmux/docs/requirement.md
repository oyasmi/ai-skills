
## 📋 方案设计任务书：面向AI Agent使用的终端自动化控制命令-agentmux

让AI Agent 能够像人类工程师一样，在复杂的终端环境（特别是 TUI 界面）中游刃有余。主要是为了让一个承担编排器角色的Agent(如openclaw)能够轻松驾驭各种强力的 Coding CLI 工具，如Claude Code、OpenCode、Codex等，它们实际是TUI界面。

### 1. 项目背景与诉求
* **背景**：当前的 AI 编程助手（如 `claudecode`、`codex`）多以 CLI/TUI（终端用户界面）形式存在。现有的 AI 框架难以处理非流式的、复杂的全屏终端交互。
* **诉求**：构建一个“AI 虚拟工作台”，让 **OpenClaw** 能够：
    1.  同时开启并管理多个专业的 Agent 进程。
    2.  能够“看见”并操作 `vim` 或 `claudecode` 这种复杂的全屏界面。
    3.  具备高并发、易部署、进程持久化的能力。

### 2. 方案选择：Golang + Tmux
经过对比 PTY（原生伪终端）与容器化方案，最终选定 **Golang + Tmux**：
* **为何选择 Golang**：
    * **单文件分发**：无依赖打包，部署极简。
    * **强并发模型**：利用 Goroutine 完美调度多个 Tmux 会话/窗格。
    * **生态库支持**：如 `github.com/owenthereal/tmux` 提供了成熟的对象化封装。
* **为何选择 Tmux**：
    * **状态引擎**：Tmux 充当了TUI的“无头浏览器”的角色，它能解析复杂的 ANSI 转义码，并在内存中维护一个“干净”的屏幕快照。
    * **持久化**：即使控制程序崩溃，Agent 进程依然在 Tmux 会话中运行，不会丢失上下文。
    * **可视化调试**：人类可以随时 `tmux attach` 进去，观察 AI 的操作过程。

### 3. 核心系统功能需求

* **控制**：负责 Session 生命周期管理、任务分发、命令输入。
* **感知**：通过 `capture-pane` 定期抓取文本快照，并进行增量更新检测，只将变化的内容推送到 LLM。
* **执行**：每个 Pane 运行一个独立的 CLI 工具。

---

### 4. 核心实现注意点（坑位预警）

* **TUI 界面清洗**：
    * 不要直接读取 stdout 流，必须使用 `tmux capture-pane`。
    * **策略**：设置 `export TERM=xterm-256color` 以保证 TUI 工具（如 ClaudeCode）能正常渲染，但抓取时使用纯文本模式。
* **交互同步问题**：
    * AI 发送指令后，最好等待屏幕内容“稳定”（无新字符产出）或检测到特定的提示符（Prompt），再进行抓取。
* **资源限制**：
    * 虽然 Tmux 很轻量，但大量并发 Agent 会消耗 CPU。建议在 Go 层实现简单的资源调度器（如最多同时活跃 8 个会话）。
* **指令反馈循环**：
    * AI 可能输入错误命令。系统需要能够识别 Tmux 中的错误提示，并将错误上下文完整反馈给 OpenClaw。

---

### 5. 方案优势总结
1.  **高容错性**：Tmux 缓冲了 TUI 的复杂性，OpenClaw 只需处理结构化的纯文本。
2.  **多代理协同**：通过 Go 的 Channel，可以轻松实现 Agent A 的输出作为 Agent B 的输入。
3.  **开发效率**：比手写终端仿真器（Terminal Emulator）快 10 倍，比 Docker 方案更轻量、实时。


### 6. 重要说明
1. 这是个命令行命令，提供的是面向AI Agent的命令行操作界面。相比对人类操作友好，对Agent的操作友好更加重要。必须支持格式化的输出(如--json)。
2. 这个工具命令命名为`agentmux`，说明它主要是为了使用tmux的方式来管理和运行agent。虽然从技术上看，在tmux中运行claudecode跟vim等其他TUI应用并无显著区别。
3. 工具在tmux的管理之外，还要加入agent管理的支持。通过tmux的一个session中运行如claude code 跑起来的是一个agent instance，它包含若干个配置项：
  - agent instance
      - model，形如<provider>/<model_name>，如openai/gpt-5.4
      - system_prompt，只在第一个prompt前面附加一次，给到LLM服务
      - prompt, 给agent的第一个指令，可以为空，意思是先启动，但是不发送（包括system_prompt也不会发送，要等到第一个prompt到来，一起发）
      - cwd，启动的工作目录，这个参数经常是要覆盖的
      - command，启动命令，形如 `claude --dangerously-skip-permissions --model $MODEL` , 这里$MODEL是前面agent template 中的model配置值，这个参数用来定义agent harness，也就是claude、opencode、codex这些
  - agent template，因为上面一堆参数不好指定，所以另外增加一个template，它是一堆参数的组合，先定义好，方便引用，在命令行指定了template就相当于制定了它所定义的一组参数，也可以在命令行继续指定具体参数，将会覆盖template中的值
4. agentmux要支持配置文件，放在 .config/agentmux.yaml ,yaml或jsonc格式都可，你来择优确定
5. 下面是我设想中的一些常见命令形式，只是用来说明我的设想，你完全可以重新设计更优的CLI界面，这非常重要：
  - agentmux list-templates，列出可用模版
  - agentmux list，列出已拉起的agent instances，等同于list-instances
  - agentmux summon --template xxx --cwd ~/project-x，召唤agent，若没有则创建，返回一个agent的instance-name，agentmux应该要生成一个可读简短有语义的name，instance-name也允许由调用方提供(通过 --name 参数)。后续的很多操作对instace的操作都应提供instance-name。也可以不使用template而提供具体的参数来拉起agent instance。所有参数都应该提供默认值，从而允许少提供参数也尽量运行的强壮性
  - agentmux attach <instance-name>，这个是给人使用的，如果没有提供<instance-name>，则提供一个列表让人选择，比如输入3，attach到具体的命令中去实时查看
  - agentmux halt <instance-name>，默认优雅停止，必要时再强制结束；支持 --immediately 直接强停
  - agentmux prompt <instance-name>,向agent提供输入，需要支持特殊键，如Enter等，这里可以参考tmux命令行的 send-keys 等
  - agentmux capture <instance-name>，抓取显示，应该能支持往前抓多屏，以获取完整的信息，你来设计
6. 这个agentmux的命令还需要提供一个 SKILL ，好让openclaw/nanobot/codex等agent能够安装此技能从而学会使用。
