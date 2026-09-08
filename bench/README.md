# codefind 基准（rg vs codefind）

本目录收录 **游戏 XLSX 数值表** 与 **多语言 `--lang` text 模式** 两套对照夹具、脚本与基线结果，供后续优化对照。

> 二进制 `codefind` 不在本目录入库（仓库根 `.gitignore` 已忽略 `/codefind`）。请先在仓库根构建，或通过环境变量指向已有二进制。

## 依赖

- 已构建的 `codefind`（默认：仓库根目录 `./codefind`，也可 `CODEFIND_BIN` / `PATH`）
- `rg`（ripgrep）在 `PATH` 上
- Python 3.10+

## 环境变量（可选）

修订脚本默认写入 `bench/results/v2/`（Git 忽略），不覆盖历史基线。`BENCH_RESULTS_DIR` 可指定独立输出目录，`BENCH_MAX_ANCHORS` 默认 12（可设 50 复现旧口径）。Windows 可直接通过 `CODEFIND_BIN` 指向 exe，或在仓库根构建 codefind.exe。

```sh
python -m unittest discover -s bench/scripts -p test_bench.py
python bench/scripts/bench_xlsx_readback.py
python bench/scripts/compare_xlsx.py --before /path/to/before --after /path/to/after --runs 10
```

v2 修复 Windows 路径、单文件计数、stderr 被当作命中和原始 argv 路径泄露；统一 rg --no-config，保留小数毫秒，并记录 OS/架构/Python/rg 版本及 binary SHA-256。工作树 Git SHA 不是任意外部二进制的构建凭证。

XLSX 的解压准备耗时单独记录，rg_unzip 仍是“已解压输入”的搜索计时，不包含坐标重建。xml_format_lines 只描述 XML 序列化，不能当相关性噪声率。readback 脚本使用独立 XML oracle 校验默认12项坐标，并从返回位置调用 read，报告覆盖、输出字节、总进程耗时和源码哈希不变；不验证业务含义，功能单次结果不作为性能基准。

| 变量 | 含义 | 默认 |
|---|---|---|
| `CODEFIND_BIN` | codefind 可执行文件路径 | 仓库根 `./codefind`，否则 `PATH` 上的 `codefind` |
| `CODEFIND_BENCH_ROOT` | 基准根目录（含 `fixtures/`、`results/`） | `bench/`（相对本仓库） |
| `BENCH_RUNS` | 每查询重复次数（取 median） | `3` |
| `BENCH_TIMEOUT` | codefind `--timeout` 秒数 | `10` |

## 1. XLSX：原生单元格 vs rg（raw / unzip）

夹具：`bench/fixtures/game_tables/survivor_game_tables.xlsx`

```bash
# 在仓库根目录
./bench/scripts/bench_rg_vs_codefind.sh
# 或
python3 bench/scripts/bench_rg_vs_codefind.py
```

产出：

- `bench/results/latest.csv` / `latest.json` / `latest.jsonl`
- `bench/results/summary.md`

要点：codefind `--format xlsx` **不调用 rg**；墙钟通常慢于 `rg_unzip`，赢面是 **sheet + A1 JSON**，不是速度。详见 `OPTIMIZATION.md`。

## 2. 多语言 text：`--lang` vs 单次 rg

夹具：`bench/fixtures/multilang_game/`（Go/Lua/TS/C#/C/C++/JS + proto/csv/yaml/md，含 vendor/min.js decoy）

```bash
./bench/scripts/bench_lang_rg_vs_codefind.sh
# 或
python3 bench/scripts/bench_lang_rg_vs_codefind.py
```

产出（**不覆盖** xlsx 的 `latest.*`）：

- `bench/results/lang_latest.csv` / `lang_latest.json`
- `bench/results/lang_summary.md`

要点：默认不传 `--lang` 时仅为 **Go**；Lua/TS-only 线索会 miss；naive rg 命中更多但夹带 vendor/min.js 噪声。详见 `OPTIMIZATION.md`。

## 目录结构

```
bench/
  README.md
  OPTIMIZATION.md
  fixtures/
    game_tables/
    multilang_game/
  scripts/
    bench_rg_vs_codefind.py|.sh
    bench_lang_rg_vs_codefind.py|.sh
  results/
    summary.md / latest.*
    lang_summary.md / lang_latest.*
```
