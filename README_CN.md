# codefind

[![CI](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml/badge.svg)](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) | 简体中文

`codefind` 是一个面向 AI Coding Agent 的有预算代码发现 CLI。它把领域词和候选符号拆成至多两次受限的 [`rg`](https://github.com/BurntSushi/ripgrep) 字面量搜索，并返回少量、可继续回读的源码锚点。

它的职责是缩小后续阅读范围，而不是判断功能是否存在。`codefind` 不是 Code Graph，也不建立语义边。

## 核心特点

- 单次进程调用，内部最多执行两次 `rg`：一组搜索领域词，一组搜索候选 symbol/test 名称。
- 所有模式均通过 `rg --fixed-strings` 按字面量处理，不解释为正则表达式或 shell 代码。
- 显式限制原始匹配数、投影锚点数和总耗时。
- 用单行 JSON 返回仓库相对路径和行号。
- 搜索目录必须位于 `--root` 内，解析 symlink 后仍禁止越界。
- 不建立索引、不启动 daemon、不调用模型，也不写入被搜索的仓库。

## 依赖

- 从源码构建需要 Go 1.22 或更高版本
- 运行时需要 `rg`（ripgrep）位于 `PATH`

可以先检查依赖：

```sh
go version
rg --version
```

## 安装

通过 Go 安装最新版本：

```sh
go install github.com/Q-xuan/codefind/cmd/codefind@latest
```

请确保 Go 的 bin 目录已经加入 `PATH`。也可以从源码构建：

```sh
git clone https://github.com/Q-xuan/codefind.git
cd codefind
go build -o codefind ./cmd/codefind
```

Windows 请把输出文件名改为 `codefind.exe`。

## 使用

在示例 Go 仓库中搜索配置加载逻辑：

```sh
codefind --root ./example-repo \
  --path cmd --path internal --path docs \
  --term "configuration" --term "load config" \
  --symbol "LoadConfig" --symbol "TestLoadConfig"
```

PowerShell：

```powershell
codefind --root .\example-repo `
  --path cmd --path internal --path docs `
  --term configuration --term "load config" `
  --symbol LoadConfig --symbol TestLoadConfig
```

必须至少提供一个 `--term` 或 `--symbol`。两个参数都可以重复，用于传入多个字面量模式。

### 参数

| 参数 | 含义 | 默认值 / 上限 |
| --- | --- | --- |
| `--root` | 要搜索的仓库根目录，必填 | 无 |
| `--path` | `root` 内的相对目录，可重复 | `.` |
| `--term` | 领域词、动作词或历史别名，可重复 | 与 `--symbol` 至少提供一项 |
| `--symbol` | 候选 symbol 或测试名，可重复 | 与 `--term` 至少提供一项 |
| `--max-anchors` | 最多输出多少个投影锚点 | 12 / 最高 50 |
| `--max-matches` | 最多读取多少条 `rg` 原始匹配 | 2000 / 最高 10000 |
| `--timeout` | 整次搜索的总超时 | 2s / 最高 10s |
| `--version` | 输出版本后退出 | - |

## JSON Contract

每个合法请求都会向 stdout 输出一行 `codefind-result-v1` JSON：

```json
{"schema_version":"codefind-result-v1","engine":"codefind","version":"0.1.0","status":"candidates_found","query":{"terms":["configuration"],"symbols":["LoadConfig"],"paths":["cmd","internal"]},"anchors":[{"kind":"source","path":"internal/config/load.go","line":12,"text":"func LoadConfig(path string) error {","groups":["symbols"]}],"unknowns":[],"metrics":{"agent_calls":1,"rg_calls":2,"elapsed_ms":8,"first_anchor_ms":3,"raw_matches":4,"projected_anchors":1,"truncated":false},"limits":{"max_anchors":12,"max_matches":2000,"timeout_ms":2000},"external_writes":0}
```

### 结果字段

- `schema_version`：结果结构版本；消费者应先检查此字段。
- `engine` / `version`：输出工具和 CLI 版本。
- `status`：机器可判定的结果状态。
- `query`：清理、去重后实际使用的词、符号和搜索目录。
- `anchors`：预算内的候选位置；`path` 始终相对 `root`。
- `unknowns`：当前结果不能回答的事项，绝不能解释为否定结论。
- `metrics`：调用次数、耗时、原始匹配、投影锚点与截断状态。没有观察到锚点时，`first_anchor_ms` 为 `null`。
- `limits`：本次请求实际采用的预算。
- `external_writes`：对目标仓库的写入次数，当前固定为 `0`。

面向人的 `text` 和 `unknowns` 文本可能变化。稳定分支应使用 `schema_version` 与 `status`，不要解析提示文本。

### 状态值

| 状态 | 含义 |
| --- | --- |
| `candidates_found` | 找到有限候选，下一步仍需回读对应源码。 |
| `no_candidates` | 当前词、路径和预算下零命中；语义是 unknown，不是功能不存在。 |
| `budget_exceeded` | 达到时间或原始匹配预算；已返回的候选可能不完整。 |
| `tool_unavailable` | 找不到 `rg`，因此没有执行代码发现。 |

### 锚点类型

| 类型 | 常见匹配 |
| --- | --- |
| `test` | Go 测试或测试目录内的文件 |
| `source` | `func`、`type`、`const`、`var` 等 Go 声明 |
| `consumer` | 其他源码使用位置和调用点 |
| `protocol` | Protocol Buffers 定义 |
| `config` | CSV 或 YAML 配置 |
| `docs` | Markdown 文档 |
| `generated` | 可识别的 Go 生成文件 |

无效请求输出 `codefind-error-v1`，状态为 `invalid_request`，进程退出码为 2。JSON 输出失败时退出码为 1。其他结果状态的退出码均为 0，所以调用方必须读取 `status`。

## 预算语义

预算是结果 Contract 的一部分：

- 只有 `--term` 时执行一次 `rg`；只有 `--symbol` 时也执行一次；两组都有时最多执行两次。
- `--max-matches` 限制从 `rg` 读取的原始匹配；`--max-anchors` 限制投影后的响应数量。
- 达到时间或原始匹配预算时返回 `budget_exceeded`。
- 投影和去重可能缩小输出，但这本身不表示预算耗尽。
- `no_candidates` 只表示当前查询没有产生锚点，永远不能转换成“未实现”或“不存在”。

## 默认搜索范围

`codefind` 搜索 Go、Protocol Buffers、Markdown、CSV 和 YAML 文件，默认排除 `.git`、`vendor`、`node_modules` 与 minified JavaScript。它不会自动扩大调用方通过 `--path` 提供的目录范围。

## 安全边界

- 查询词作为 `rg` 进程参数传递，不经过 shell。
- 在搜索路径前传入 `--` 选项终止符，避免短横线开头的路径变成 `rg` 选项。
- 拒绝绝对搜索路径和任何逃逸 `root` 的路径。
- 在目录包含性检查之前解析 symlink。
- 输出可能包含源码片段；搜索私有仓库时应把 JSON 结果视为敏感数据。

## 非目标

以下能力明确不属于 `codefind` v0.1.x：

- Code Graph、调用图或语义边
- 持久化索引、增量索引或后台 daemon
- embedding、RAG、向量数据库或模型推理
- MCP server、插件系统或编辑器集成框架
- 判断代码是否存在、正确或可以安全修改等业务结论
- 编辑、生成或修复目标仓库中的文件

## 开发与验证

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/codefind
```

贡献说明见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题报告方式见 [SECURITY.md](SECURITY.md)。

## License

[MIT](LICENSE)
