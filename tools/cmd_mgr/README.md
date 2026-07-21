# CmdMgr (Command Manager)

CmdMgr 是一个跨平台 GUI 命令管理工具，允许用户保存、管理并执行常用的 Shell 命令。支持服务型（Long-running）和一次性（One-shot）任务。

## 平台实现

本项目提供两个独立实现，共享相同的 SQLite 数据库 Schema：

| 目录 | 技术栈 | 目标平台 | 分发包体积 |
|---|---|---|---|
| `macos/` | Swift + SwiftUI + SQLite3 C API | macOS 13+ | ~2MB (.app) |
| `windows/` | C# + WPF + Microsoft.Data.Sqlite (.NET 8) | Windows 10+ | ~20MB (self-contained .exe) |

## 功能特性

*   **任务类型**:
    *   **Long-running**: Web 服务、后台进程等。支持启动/停止控制和实时日志查看。
    *   **One-shot**: 脚本、工具命令等。支持查看历史执行记录和输出。
*   **持久化存储**: SQLite 数据库保存命令配置和执行历史。
    *   macOS: `~/Library/Application Support/CmdMgr/cmd_mgr.db`
    *   Windows: `%APPDATA%\CmdMgr\cmd_mgr.db`

## 构建

### macOS (Swift)

```bash
cd macos && bash build.sh
```
产物: `macos/dist/CmdMgr.app`

### Windows (C#/WPF)

需要 .NET 8 SDK。

```bat
cd windows
build.bat
```
产物: `windows\dist\CmdMgr.exe`

## 数据库兼容性

两个实现共享相同的 SQLite 数据库 Schema，数据文件可以在不同实现间互通。
