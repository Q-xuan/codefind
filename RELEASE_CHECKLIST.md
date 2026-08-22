# v0.1.0 Release Checklist

本文件集中记录发布前检查；它不是自动发布框架。

## 仓库命名

- [x] GitHub 仓库：`Q-xuan/codefind`
- [x] Canonical Go module path：`github.com/Q-xuan/codefind`

## 发布前

- [ ] 确认 `git status` 只包含预期文件。
- [ ] 运行 `go fmt ./...`，并确认没有 diff。
- [ ] 运行 `go test ./...`、`go vet ./...`、`go build ./cmd/codefind`。
- [ ] 在 Windows、Linux、macOS CI 中确认 `rg` 可用且全部 job 通过。
- [ ] 扫描凭据、私钥、本机绝对路径、私有仓库名和真实业务样例。
- [ ] 把 CHANGELOG 的 `Unreleased` 替换为发布日期。
- [ ] 确认 `codefind --version` 输出 `0.1.0`。
- [ ] 创建并审查首个提交；确认 MIT License 与提交作者信息。

## 发布

- [ ] 创建公开 remote 并推送（需要仓库 owner 授权）。
- [ ] 创建带注释的 `v0.1.0` tag 并推送。
- [ ] 从 tag 重新运行 CI。
- [ ] 发布简短 release notes，链接 CHANGELOG；本版本不要求二进制发布框架。

## 发布后

- [ ] 从干净目录验证 README 的 clone/build 示例。
- [ ] 验证 `go install github.com/Q-xuan/codefind/cmd/codefind@v0.1.0`。
- [ ] 启用私密漏洞报告渠道。
