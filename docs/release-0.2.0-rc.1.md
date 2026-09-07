# 0.2.0-rc.1 本地收口记录

## 范围

本次为 Windows amd64 本地预发布包与 下游私有项目 接入，不创建 Git commit/tag、不推送或发布远程 release。当前新增功能为游戏内容定位、显式文本编码、XLSX 搜索与渐进式 read。公开安装的已发布 revision 不等于本次 working-tree 构建。

## 验证

- `go test ./... -count=1 -cover` 通过；CLI 覆盖率 92.0%，核心包 88.9%（本轮全量观测，后补边界用例亦单独通过）。
- `go vet ./...`、gofmt、`git diff --check` 通过。
- 实际 Windows 打包二进制通过 `TestCLIEndToEnd`：未知位置搜索、使用返回坐标回读、裁剪状态、非法请求 JSON、原 XLSX 哈希不变。
- 包内源码快照在相同 Go 版本、GOOS/GOARCH/CGO_ENABLED 和构建 flags 下，重建二进制哈希一致。
- Linux amd64、macOS arm64 交叉编译通过；不是 native runtime/CI 验证。
- 下游私有项目 原 text Shadow adapter 10/10 通过，真实 binary smoke 未跳过；旧 v1 与附加字段兼容，XLSX 坐标仍拒绝。
- 原 Excel Catalog 测试 3/3 通过；更新后的 extract-game-design-excel skill 使用现有 Python UTF-8 模式通过 quick_validate。
- 下游私有项目 副本的真实 XLSX 搜索返回默认12个候选，按新位置回读成功；已确认目标工作簿回读前后 SHA-256 相同。
- 对打包输入进行了本机绝对路径、私有域名与常见密钥标记的模式扫描，无发现；这不是穷尽式安全认证。

## 审查修复

- Go //line 指令不再让 syntax 行号偏离 rg 的物理行号。
- 帮助文本写入失败返回失败码。
- 回读选择及输出阶段检查 context；拒绝非规范 A01 地址，避免坐标别名导致静默漏读。
- JSON 兼容边界和 read 生产者版本单独记录。

## 接入选择

下游私有项目 的 `cli/codefind/codefind.exe` 与发布包 binary SHA 相同。code-discovery resolver 在显式 binary、CODEFIND_BIN、PATH 都不可用时回退项目副本；不改全局 PATH、Registry 或业务代码。

Excel skill 新增按需 discovery/direct 引用；格式、图片、持久证据、diff 与完整反证仍走原 Targeted/Full。旧 text-only adapter 不接收 XLSX/read 数据。策划工作簿仍是 structure_rule_reference，本地 data/tables 仍是 runtime_authority。

## 保留的边界

- Windows 当前进程缺少创建符号链接权限，新增真实 symlink-escape 用例在本机明确 SKIP；需 Linux/macOS 或具备权限的 Windows CI 执行。现有路径校验与非法路径 E2E 已通过。
- 原始策划表被其他程序打开时，常规文件哈希 API 可能受共享模式限制；使用只读共享流验证了目标表。另一次全目录查询受到预算限制，重试成功；不保证每次搜索固定耗时。
- 不承诺对恶意并发修改的文件系统提供 OS sandbox/snapshot isolation，不提供 OCR、格式渲染、公式重算或业务语义裁决。
- 公开发布前仍需 native 三平台 CI，以及用户明确授权的提交、tag、push 和远程安装验证。

## 包与复现

`tools/package.ps1` 生成 ZIP、ZIP SHA-256 和 manifest；支持 OutputRoot 用于验证包，拒绝无检查覆盖同名产物。清单记录 working-tree 来源、base commit、源文件摘要、Go 版本及构建环境；包内 source 可重建。最终包的具体摘要以生成文件为准，不手抄到本记录。
