# Contributing to codefind

感谢你帮助改进 `codefind`。项目刻意保持为一个小型、有预算的代码发现 CLI；提交功能建议前，请先阅读 README 中的“非目标”。

## 开发环境

- Go 1.22+
- `rg`（ripgrep）位于 `PATH`

提交变更前请运行：

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/codefind
```

新增行为应附带聚焦测试。测试数据必须是通用、虚构且最小的，不得包含真实业务代码、用户数据、凭据、本机绝对路径或私有仓库 Artifact。

## 变更范围

- 优先修复明确问题和改善现有 JSON Contract、预算约束、跨平台行为或文档。
- 兼容性变更必须说明对 `schema_version`、状态枚举和退出码的影响。
- Code Graph、索引、daemon、RAG、MCP 和插件系统不在 v0.1.x 范围内。
- 一个 pull request 只解决一个清晰问题，并说明验证结果。

English contributions are welcome. Please keep changes focused, include tests for behavior changes, and avoid real private-repository data in fixtures or examples.
