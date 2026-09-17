# codefind

[![CI](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml/badge.svg)](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) | 简体中文

本地预发布版本：**0.2.0-rc.1**。兼容性见 [JSON 契约](docs/json-contract.md)，发布状态见 [检查清单](RELEASE_CHECKLIST.md)。下方公开安装命令使用已发布 revision，不保证已经包含本地候选版本。

`codefind` 是一个面向游戏项目、供 AI Coding Agent 使用的有预算业务线索发现 CLI。它跨 Go 源码、Proto 协议、CSV / YAML 配置和 Markdown 文档搜索玩法名称、配置 ID、历史别名及候选符号，不依赖代码建图。

文本模式使用至多两次受限的 [`rg`](https://github.com/BurntSushi/ripgrep) 字面量搜索；XLSX 模式原生读取工作簿。两者均返回少量可继续回读的位置证据，帮助 Agent 缩小阅读范围。

它的职责是缩小后续阅读范围，而不是判断功能是否存在。`codefind` 不是 Code Graph，也不建立语义边。

## 为什么面向游戏项目

一个玩法的实现线索可能分散在服务端逻辑、网络协议、数值表、功能开关和设计文档中，而不只存在于函数调用关系里。

- **跨内容类型定位**：用玩法名、协议名或配置 ID，在源码、协议、配置和文档中寻找字面量命中的候选。
- **不要求属于代码包或 Git 仓库**：普通目录中的受支持文本文件也可以搜索，无需编译或建立索引。
- **直接读取当前落盘内容**：适合 Agent 在修改玩法逻辑前，先定位实现、测试和数据线索。
- **保留证据边界**：共同命中不证明业务关联，零命中不证明功能不存在；候选仍需回读确认。

当前更适合以 Go 为主要逻辑语言、配合 Proto 和 CSV / YAML 的游戏项目；并不覆盖所有游戏引擎语言或二进制资产，也不替代类型感知的调用图和影响分析。

## 核心特点

### XLSX 策划工作簿

使用独立的只读模式搜索 Excel 单元格存储值（包括公式缓存值）和传统批注：

```sh
codefind --root ./design/math --format xlsx --term Pvp --term "挑战券" --timeout 10s
```

此模式不调用 rg，不要求安装 Excel，不建立索引。命中返回 `kind: config`、工作簿相对 `path`、`text`、`groups` 和 `workbook: {sheet, cell, source}`；`source` 为 `cell` 或 `comment`，不提供源码 `line`。同一位置的单元格和批注可分别命中；没有批注命中不表示没有批注。`query.format` 区分 `text` / `xlsx`。

不知道文件名时，对目录跑一次即可。廉价发现按文件名、表名、共享字符串字面量排序（并列取较小文件），然后**只内容扫描第一簿**。其余文件保持 `pending`，`reason=deferred`，用 `--path` 点名下一簿。不要先排除 7–9MB 再扫剩下的目录——2–3MB 表同样会吃掉均分超时。`--path` 仍可点名单个 `.xlsx` 或 `!` 排除；点名多簿会全部内容扫描。timeout **上限仍是 10s**，`20s` 仍是 `invalid_request`。不做近义自动命中。名称/表名匹配忽略大小写且只影响排序；共享字符串提示和正文仍是大小写敏感字面量。提示未命中不会排除文件。选中的工作簿内部仍优先扫名称匹配的表。`codefind --help-xlsx` 说明目录冷启动、`read` 的 field/range，以及零命中措辞。

输出候选采用两层轮询：在已命中的工作簿之间分配名额，每个工作簿内部再轮询已命中的工作表；同表内部保留原有相关性顺序。这样可减少单表重复命中挤占结果，但不保证所有表都能进入很小的输出预算。此策略只筛选已扫描到的候选，不增加扫描范围或预算；为保持来源多样性，跨表结果不保证严格按符号优先级排列。

`workbook_coverage.files` 按扫描顺序列出已发现文件：`complete` 表示支持的内容已扫描完成，`partial` 表示未完成，`pending` 表示尚未开始。`reason` 区分 `file_timeout`、`total_timeout`、`match_limit`、`file_size_or_xml_limit`，以及目录冷启动未选中的 `deferred`。`deferred` 不是超时，单独不会变成 `budget_exceeded`。`metadata_status` 单独报告名称元数据是否读取成功。`discovery_complete: false` 表示文件枚举本身受限，列表不是全部文件。这里的 complete 不代表 OCR、公式或业务理解已完成。

元数据阶段最多使用总超时的 20%，每个工作簿最多 200ms、XML 读取最多 1 MiB（表名 + 共享字符串字面量提示）。读取失败只回退到文件名/大小排序，不丢弃候选。目录冷启动把剩余时间整段给选中的那一簿；显式点名的多簿仍按 `min(点名数, 4)` 均分。点名多簿时，单文件时间或大小预算耗尽后继续下一簿；全局时间或匹配预算耗尽后停止。故意 `deferred` 的文件不算扫描不完整。当前不提供断点续扫，重试会重新读取。

边界与预算：

- 默认仍为 `--format text`；两种模式分次查询，XLSX 不接受非 auto 的 `--encoding`。
- XLSX 使用大小写敏感的字面量匹配；不搜索工作表名、图片、截图、线程评论或公式表达式，不重算公式，也不解释字段。缓存值可能过期，数字/日期格式不渲染，合并单元格只定位实际存储值的单元格。
- 共享 `--timeout`、`--max-matches`、`--max-anchors`；原始匹配按单元格或批注计数，重复查询组不重复计数。最多扫描 32 个工作簿，单文件不超过 32 MiB，每个工作簿累计 XML 解压读取上限 64 MiB。超过任一预算返回 `budget_exceeded`，已找到的候选仍保留。
- `metrics.xlsx_files_scanned` 记录尝试扫描的文件数；损坏或不可读工作簿返回 `execution_error`，不会伪装成零命中。
- 只遍历明确授权的 root/path；不跟随符号链接，跳过点号子目录、vendor、node_modules 和 `~$` 锁文件。此原生模式不读取 `.gitignore`，请使用 `--path` 限定（目录、点名文件或 `!` 排除）；不提取 ZIP 到磁盘、不访问工作簿外部链接、不修改原表。
- 旧 `.xls`、加密工作簿和截图渲染不支持。零字面命中是 **unknown**，不能写成「表里没有该字段」。不会推断近义写法。

### 先取证，再理解

codefind 不预先建图或建立向量索引：它直接搜索当前落盘文本，把查询范围、候选筛选和输出预算交给工具，把业务理解和下一步探索留给 Agent。建议采用“搜索 → 回读 → 根据新线索再次搜索”的循环；最多两次 rg 是单次请求的限制，不代表整个调查已完成。

例如，先在文档里搜索玩法名，回读确认配置 ID，再在已授权的配置目录里查询该 ID。共同命中只是候选证据，不代表工具已经证明跨文件业务关系。

### 搜索与候选输出

- 单次进程调用，内部最多执行两次 `rg`：一组搜索领域词，一组搜索候选 symbol/test 名称。
- 所有模式均通过 `rg --fixed-strings` 按字面量处理，不解释为正则表达式或 shell 代码。
- 显式限制原始匹配数、投影锚点数和总耗时。
- 用单行 JSON 返回仓库相对路径和行号。
- Go 候选可以携带受限的 `go/ast` 语法证据（`definition`、`call`、`reference`），但不会冒充类型解析后的关系边。
- 搜索目录必须位于 `--root` 内，解析 symlink 后仍禁止越界。
- 不建立索引、不启动 daemon、不调用模型，也不写入被搜索的仓库。
- 文件类型优先于目录名分类，避免把 proto 目录里的 Markdown 文档或 Go 逻辑误当成协议。
- 同长度字面量的排序中，纯数字查询优先完整数字命中（数字两侧不是其他数字），减少配置 ID 的子串噪声；不保证 CSV 字段相等，子串候选仍保留。较长匹配、分组优先级和分类配额仍会影响最终顺序。

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

### 多语言源码搜索

默认保留 Go 源码范围以兼容已有调用；用可重复的 `--lang` 选择其他语言，或 `--lang all` 开启全部支持语言：

```sh
codefind --root ./game-project --lang lua --lang ts --symbol ClaimReward
codefind --root ./game-project --lang all --term "reward"
```

| 语言 | 参数 | 文件后缀 |
| --- | --- | --- |
| Go | go（默认） | .go |
| Lua | lua | .lua |
| C# | csharp（别名 cs、c#） | .cs |
| C | c | .c、.h |
| C++ | cpp（别名 c++） | .cpp、.cc、.cxx、.h、.hpp、.hh、.hxx、.C |
| JavaScript | js（别名 javascript） | .js、.jsx、.mjs、.cjs |
| TypeScript | ts（别名 typescript） | .ts、.tsx、.mts、.cts |

语言选择只限制源码类型，仍搜索 Proto、Markdown、CSV、YAML；`--path` 可继续缩小目录，或点名/排除单个文件。JS minified 文件、vendor 和 node_modules 继续排除。不识别的语言报 invalid_request，xlsx 模式拒绝 --lang。`query.languages` 返回去重、规范化后的实际语言列表；xlsx 返回空列表。

除 Go 外均为字面量源码候选，不标注 AST、定义或调用关系；`source` 在这些语言中仅表示源码文本，测试目录内归为 `test`。Go 保留原有 AST 证据。不新增编译器、索引或语言服务，所有语言共享原有搜索次数与预算。

### 搜索之后渐进式回读

找到 XLSX 位置后，使用 `read` 子命令，只读取明确授权的单工作簿和工作表：

```sh
codefind read --root ./design/math --file common.xlsx --sheet common --range B6:AD20
codefind read --root ./design/math --file common.xlsx --sheet common --anchor D20 --field "参数1" --field param1
```

文件名、工作表和坐标均需替换为实际命中位置。`--range` 与 `--anchor` 二选一。矩形已知时 **优先 `--range`**：`--field 类型` 可能选中旁边同名列。`codefind --help-xlsx` 与 `codefind read --help` 写明这一点。

- 显式 range 最多 4096 格；`--max-cells` 默认返回 96 格、上限 4096；`--max-chars` 默认 24000、上限 200000，限制返回的原始文本字符，不包含 JSON/坐标开销。空格子不逐一返回。
- anchor 默认 `--strategy adaptive`，需要 `--field` 指定目标字段及可重复别名。结构策略在前 64 列寻找同行相同键，在键右侧向上最多 32 行匹配字段表头；未找到字段列时回退同行＋前 12 行表头。不是自动字段解释，多个字段候选全部保留。
- 可显式使用 `structure`、`row_headers` 或 `window`；window 为命中格上下 2 行、左右 4 列。策略都不自动跨工作表或扩大授权范围。
- adaptive 向上未找到字段时，先在命中格下方 8 行、前 64 列寻找字段表头；命中后回读该列自表头起的 9 行，并带上命中格所在列及右侧两列的同行标签。实际策略报告 below_headers，原因 no_field_columns_above；下方仍无字段才回退 row_headers。多列候选全部保留，此规则不证明标题与附近表格的业务关联。为此 adaptive 保留的候选范围最多延伸至命中格下方 16 行，仍受输出与总读取预算约束。
- 结果结构为 `codefind-read-v1`，包含 cells、field_candidates、merged_ranges、实际 strategy、fallback_reason 和 coverage。单元格分别保留 value、formula、cached_value、comment；共享公式保留原始属性，不重建表达式，不渲染数字格式、不重算。
- `read_complete` 仅说明选定范围解析及输出未被预算截断，不表示问题已回答；`budget_exceeded` 表示读取或输出未完成。错误沿用 JSON invalid_request/退出码2、execution_error/退出码1，成功结果（含预算耗尽）退出码0。
- 超时默认 2s、上限 10s；沿用工作簿 32 MiB、XML 64 MiB 限制，最多扫描 100000 个**范围内**单元格/批注事件和 4096 个合并区域。选定 range 或 anchor 框外的格子会跳过，不计入该上限。工作簿共享字符串仍会读取。coverage.ranges 是保留候选的坐标范围，不是磁盘读取范围。
- 合并区域保留坐标；左上角在保留范围外时提示扩大范围，不擅自补读。文本裁剪用 text_truncated 标记；批注属于 cell，不与别处的备注文字混为一谈。

建议流程：搜索定位 → 结构回读 → 必要时显式扩大范围 → 仍不足再查其他表/文档。说明是否足够由 Agent 判断，工具不自动跑完整调查。

在示例游戏项目中定位竞技场奖励相关线索（请将目录和查询词替换为项目中实际存在的内容）：

```sh
codefind --root ./game-project \
  --path internal --path proto --path config --path docs \
  --term "arena_reward" --term "100126" \
  --symbol "ClaimArenaReward" --symbol "TestClaimArenaReward"
```

PowerShell：

```powershell
codefind --root .\game-project `
  --path internal --path proto --path config --path docs `
  --term arena_reward --term "100126" `
  --symbol ClaimArenaReward --symbol TestClaimArenaReward
```

必须至少提供一个 `--term` 或 `--symbol`。两个参数都可以重复，用于传入多个字面量模式。

### 参数

| 参数 | 含义 | 默认值 / 上限 |
| --- | --- | --- |
| `--root` | 要搜索的根目录，不要求是 Git 仓库，必填 | 无 |
| `--path` | `root` 内相对目录或单个文件，可重复；`!` 前缀排除 | `.` |
| `--term` | 领域词、动作词或历史别名，可重复 | 与 `--symbol` 至少提供一项 |
| `--symbol` | 候选 symbol 或测试名，可重复 | 与 `--term` 至少提供一项 |
| `--max-anchors` | 最多输出多少个投影锚点 | 12 / 最高 50 |
| `--max-matches` | 最多读取多少条 `rg` 原始匹配 | 2000 / 最高 10000 |
| `--timeout` | 整次搜索的总超时。大 xlsx 先用 `--path` 过滤，不要加到 10s 以上 | 2s / 最高 10s |
| `--encoding` | 本次搜索的文件编码：`auto`、`utf-8`、`gbk`、`gb18030` | `auto` |
| `--format` | 搜索模式：`text` 或 `xlsx` | `text` |
| `--lang` | 源码语言，可重复；`all` 选择全部，详见上表 | `go` |
| `--version` | 输出版本后退出 | - |
| `--help-xlsx` | 说明 xlsx 的 `--path`、`read` 的 field/range，以及零命中 unknown | - |

## JSON Contract

### 配置表编码

默认 `auto` 沿用 rg 的编码行为（含 BOM 检测），不自动猜测 GBK，也不会在零命中后更换编码重试。GBK 策划表可显式指定：

```sh
codefind --root ./game-project --path data/tables --term "奖励" --encoding gbk
```

编码作用于本次请求的所有搜索目录；UTF-8 源码与 GBK 配置表应分次查询。查询词及 JSON 输出仍使用 Unicode / UTF-8，目标文件不会被转码或修改。Go AST 仍按 Go 源文件规则解析，非 UTF-8 Go 文件解析失败时保持纯文本候选。结果的 `query.encoding` 记录规范化后的所选编码，而不是对每个文件编码的检测结论。

每个合法请求都会向 stdout 输出一行 `codefind-result-v1` JSON：

```json
{"schema_version":"codefind-result-v1","engine":"codefind","version":"0.2.0-rc.1","status":"candidates_found","query":{"format":"text","encoding":"auto","terms":["configuration"],"symbols":["LoadConfig"],"paths":["cmd","internal"]},"anchors":[{"kind":"source","path":"internal/config/load.go","line":12,"text":"func LoadConfig(path string) error {","groups":["symbols"],"syntax":{"role":"definition","symbol":"LoadConfig","authority":"go_ast_syntax"}}],"unknowns":[],"metrics":{"agent_calls":1,"rg_calls":2,"elapsed_ms":8,"first_anchor_ms":3,"raw_matches":4,"projected_anchors":1,"truncated":false,"syntax_files_parsed":1,"syntax_anchors":1,"syntax_parse_errors":0,"syntax_files_skipped":0},"limits":{"max_anchors":12,"max_matches":2000,"timeout_ms":2000},"external_writes":0}
```

### 结果字段

- `schema_version`：结果结构版本；消费者应先检查此字段。
- `engine` / `version`：输出工具和 CLI 版本。
- `status`：机器可判定的结果状态。
- `query`：清理、去重后实际使用的词、符号和搜索目录。
- `query.encoding`：本次采用的文件编码选项，默认 `auto`。
- `anchors`：预算内的候选位置；`path` 始终相对 `root`。可选 `syntax` 是 `go/ast` 提供的语法级证据，不是类型解析关系。
- `unknowns`：当前结果不能回答的事项，绝不能解释为否定结论。
- `metrics`：调用次数、耗时、原始匹配、投影锚点、截断状态和受限 Go 语法解析计数。没有观察到锚点时，`first_anchor_ms` 为 `null`。
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
| `source` | Go 声明，或其他所选语言的普通源码候选（不证明定义） |
| `consumer` | 其他源码使用位置和调用点 |
| `protocol` | Protocol Buffers 定义 |
| `config` | CSV 或 YAML 配置 |
| `docs` | Markdown 文档 |
| `generated` | 可识别的 Go 生成文件 |

无效请求（包括参数格式错误和多余的位置参数）输出 `codefind-error-v1`，状态为 `invalid_request`，进程退出码为 2。搜索执行失败使用同一错误结构，状态为 `execution_error`，退出码为 1。JSON 输出失败时退出码为 1。其他结果状态的退出码均为 0，所以调用方必须读取 `status`。帮助和版本请求输出纯文本。

## 预算语义

预算是结果 Contract 的一部分：

- 只有 `--term` 时执行一次 `rg`；只有 `--symbol` 时也执行一次；两组都有时最多执行两次。
- `--max-matches` 限制从 `rg` 读取的原始匹配；`--max-anchors` 限制投影后的响应数量。
- 达到时间或原始匹配预算时返回 `budget_exceeded`。
- 投影和去重可能缩小输出，但这本身不表示预算耗尽。
- 优先搜索 symbols，再使用剩余的共享匹配预算搜索 terms；symbols 耗尽预算时可能跳过 terms。
- `metrics.truncated` 表示预算耗尽或有去重后的候选未输出；重复匹配本身不会触发此标记。
- `rg` 使用 `--no-config`，忽略 `RIPGREP_CONFIG_PATH`，避免用户配置改变搜索约定。
- Go 语法增强与搜索共用本次请求 timeout，只解析 lexical shortlist，最多 64 个文件且单文件不超过 1 MiB；解析失败保持 lexical-only，并通过 metrics 计数。
- `no_candidates` 只表示当前查询没有产生锚点，永远不能转换成“未实现”或“不存在”。

## 默认搜索范围

`text` 模式默认搜索 Go、Protocol Buffers、Markdown、CSV 和 YAML；`--lang` 可选择额外支持的源码语言。默认排除 `.git`、`vendor`、`node_modules` 与 minified JavaScript。`xlsx` 模式的范围规则见上文。它不会自动扩大调用方通过 `--path` 提供的目录或文件范围。

每次请求只接受一个 `--root`。多个仓库或普通目录位于同一授权根目录下时，可以通过多个 `--path` 搜索；不支持一次指定任意分散的多个根目录。搜索仍遵循适用的 `.gitignore` 等 ripgrep 忽略规则，不保证枚举根目录下的所有文件。

## 安全边界

- 查询词作为 `rg` 进程参数传递，不经过 shell。
- 在搜索路径前传入 `--` 选项终止符，避免短横线开头的路径变成 `rg` 选项。
- 拒绝绝对搜索路径和任何逃逸 `root` 的路径。
- 在目录包含性检查之前解析 symlink。
- 输出可能包含源码片段；搜索私有仓库时应把 JSON 结果视为敏感数据。

## 非目标

以下能力明确不属于 `codefind` v0.1.x：

- Code Graph、调用图或语义边
- 类型解析后的 receiver、interface dispatch、reflection 或运行时调用结论；`go_ast_syntax` 只描述源码语法
- 持久化索引、增量索引或后台 daemon
- embedding、RAG、向量数据库或模型推理
- MCP server、插件系统或编辑器集成框架
- 判断代码是否存在、正确或可以安全修改等业务结论
- 编辑、生成或修复目标仓库中的文件

## 开发与验证

游戏场景回归覆盖源码、协议、CSV / YAML、文档、生成代码、数字 ID 排序，以及非 Git 目录下分两次查询的范围约束：

```sh
go test ./internal/find -run TestGame -count=1 -v
```

这些是合成场景回归，不是实际游戏项目的检索质量评测，也不能据此声称优于原生 rg、Graph 或向量检索。

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/codefind
```

贡献说明见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题报告方式见 [SECURITY.md](SECURITY.md)。

## License

[MIT](LICENSE)
