# Security Policy

## Supported versions

安全修复仅面向最新发布版本。v0.x 阶段可能包含不兼容修复，升级前请阅读 CHANGELOG。

## Reporting a vulnerability

请不要在公开 Issue 中披露可利用细节、凭据或私有仓库内容。公开托管后，优先使用代码托管平台提供的私密漏洞报告功能。如果该功能尚未启用，请只创建一个不含敏感细节的 Issue，请求维护者提供私密联系渠道。

报告建议包含受影响版本、操作系统、最小复现步骤、影响范围和建议修复方向。维护者确认安全的披露方式后，再发送日志或样例；发送前请移除 token、绝对路径和业务源码。

`codefind` 会启动系统中的 `rg` 并读取调用者授权的目录。请只对可信工作区运行，并把 JSON 输出视为可能包含源码片段的敏感数据。

English: Please do not disclose exploit details or private source in a public issue. Use the hosting platform's private vulnerability-reporting channel when available.
