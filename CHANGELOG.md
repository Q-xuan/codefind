# Changelog

本项目的重要变更记录在此文件中，格式参考 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，版本遵循 [Semantic Versioning](https://semver.org/)。

## [0.1.0] - Unreleased

### Added

- 面向 AI Coding Agent 的有预算字面量代码发现 CLI。
- `codefind-result-v1` JSON Contract，以及候选、零命中、预算超限和工具不可用状态。
- 搜索根目录与 symlink 越界检查。
- Windows、Linux、macOS CI 验证。

### Security

- 限制搜索路径、执行次数、原始匹配数、输出锚点数和总超时。
- 目标仓库保持只读，结果只返回相对路径。
