# codefind `--lang` vs 单次 rg：多语言游戏夹具基准

- 夹具：`bench/fixtures/multilang_game`
- 构建：codefind `0.2.0-rc.1` / git `7de8eb1` — feat(search): 增加游戏项目多语言源码搜索
- 方法：
  - **rg_single_naive**：单次 `rg -F -n`，无语言/排除纪律（代理常见写法）
  - **rg_single_scoped**：单次 `rg -F -n` + 与 codefind 对齐的语言 glob + proto/md/csv/yaml + 排除 vendor/node_modules/*.min.js
  - **codefind_default**：不传 `--lang`（默认 **Go only**，域文件仍可搜）
  - **codefind_lang_lua_ts**：`--lang lua --lang ts`
  - **codefind_lang_all**：`--lang all`
- 每查询跑 3 次取 **median** 墙钟（ms）
- **未覆盖** xlsx `latest.*`；本文件为 `lang_*`

## 夹具埋点

| 路径 | 线索 |
|---|---|
| `internal/reward/claim.go` | ClaimArenaReward / arena_reward / 100126（Go AST） |
| `client/lua/arena_reward.lua` | 同上 + `LUA_ONLY_CLUE_MoonlitArenaChest` |
| `client/ts/arenaReward.ts` | 同上 + `TS_ONLY_CLUE_SeasonPassArenaBadge` |
| `client/csharp` / `engine/c` / `engine/cpp` / `client/js` | 共享符号/词/ID |
| `proto` / `config` / `docs` | 域证据（默认语言收窄时仍可搜） |
| `vendor` / `node_modules` / `*.min.js` | 仅 decoy，应被排除 |

## 结果表（median）

| 查询 | 极性 | rg_naive ms/hits/noise | rg_scoped ms/hits/noise | cf_default ms/hits/syn/status/langs | cf_lua_ts ms/hits/syn/status | cf_all ms/hits/syn/status |
|---|---|---|---|---|---|---|
| `invalid_lang foobar` | neg | — | — | **status=`invalid_request`** ms=1 ec=2 | — | — |
| `symbol_ClaimArenaReward` (`ClaimArenaReward`) | pos | 4 / 27 / 3 | 4 / 24 / 0 | 7 / 10 / syn=1 / `candidates_found` / [go] | 6 / 12 / syn=0 / `candidates_found` / [lua,ts] | 6 / 24 / syn=1 / `candidates_found` / [go,lua,csharp,c,cpp,js,ts] |
| `term_arena_reward` (`arena_reward`) | pos | 4 / 24 / 3 | 4 / 21 / 0 | 6 / 6 / syn=0 / `candidates_found` / [go] | 6 / 8 / syn=0 / `candidates_found` / [lua,ts] | 7 / 21 / syn=0 / `candidates_found` / [go,lua,csharp,c,cpp,js,ts] |
| `id_100126` (`100126`) | pos | 3 / 22 / 2 | 4 / 20 / 0 | 6 / 7 / syn=0 / `candidates_found` / [go] | 6 / 9 / syn=0 / `candidates_found` / [lua,ts] | 6 / 20 / syn=0 / `candidates_found` / [go,lua,csharp,c,cpp,js,ts] |
| `lua_only` (`LUA_ONLY_CLUE_MoonlitArenaChest`) | pos | 4 / 1 / 0 | 4 / 1 / 0 | 6 / 0 / syn=0 / `no_candidates` / [go] | 6 / 1 / syn=0 / `candidates_found` / [lua,ts] | 6 / 1 / syn=0 / `candidates_found` / [go,lua,csharp,c,cpp,js,ts] |
| `ts_only` (`TS_ONLY_CLUE_SeasonPassArenaBadge`) | pos | 5 / 1 / 0 | 4 / 1 / 0 | 6 / 0 / syn=0 / `no_candidates` / [go] | 6 / 1 / syn=0 / `candidates_found` / [lua,ts] | 6 / 1 / syn=0 / `candidates_found` / [go,lua,csharp,c,cpp,js,ts] |
| `neg_NotInTreeAnywhere` (`NotInTreeAnywhere_ZZZ999`) | neg | 4 / 0 / 0 | 4 / 0 / 0 | 6 / 0 / syn=0 / `no_candidates` / [go] | 6 / 0 / syn=0 / `no_candidates` / [lua,ts] | 6 / 0 / syn=0 / `no_candidates` / [go,lua,csharp,c,cpp,js,ts] |
| `decoy_vendor_should_exclude` (`DECOY_ONLY_IN_VENDOR_ShouldNeverSurface`) | neg | 4 / 1 / 1 | 4 / 0 / 0 | 6 / 0 / syn=0 / `no_candidates` / [go] | 6 / 0 / syn=0 / `no_candidates` / [lua,ts] | 6 / 0 / syn=0 / `no_candidates` / [go,lua,csharp,c,cpp,js,ts] |

## 能力差距（产品真相）

1. **默认 = Go**：不传 `--lang` 时 `query.languages=["go"]`。非 Go 源码中的线索（Lua/TS only）在 default 下为 **0 源码命中**；域文件（proto/md/csv/yaml）仍可能命中共享词。
2. **`--lang` 是显式 opt-in**：`--lang lua --lang ts` 或 `--lang all` 才能打开对应扩展；未知 lang → `invalid_request`（exit 2）。
3. **非 Go 只有词法候选**：`kind` 为 source/test/config 等，**无 `syntax` AST**；仅 Go 命中可带 `go_ast_syntax`。
4. **结构化合同**：codefind 输出 path+line JSON、`metrics.rg_calls`（text 模式 ≤2）、limits 预算；单次 rg 有 path:line 文本但 **无 kind / syntax / budget 字段**。
5. **rg 能搜到字面量，但纪律靠人**：naive rg 会打进 vendor/node_modules/*.min.js（noise）；scoped rg 接近 codefind 文件范围，仍无 AST/kind 合同。
6. **zero ≠ absent**：负例与 default 漏掉非 Go 线索时，`no_candidates` / 0 命中只表示 unknown，不能断言功能不存在——需扩大 `--lang` 或换稳定 symbol。
7. **xlsx 拒绝 `--lang`**：本基准为 text 模式；xlsx 对比见既有 `summary.md` / `latest.*`（未改写）。


## 读数说明

- **hits 口径不同**：rg 按匹配行计；codefind 按 projected anchors 计（本基准 `--max-anchors 50`）。共享线索上 scoped rg 与 `codefind --lang all` 数量接近（如 ClaimArenaReward 24/24），但语义合同不同。
- **default 的非零命中**：对共享词/ID，default（Go）仍可能命中 **Go 源码 + 域文件**（proto/md/csv/yaml），不等于搜到了 Lua/TS 源码。Lua/TS-only 线索在 default 下为 `no_candidates`。
- **syn 列**：`has_syntax=1` 仅当锚点含 Go AST（`go_ast_syntax`）。`--lang lua --lang ts` 不含 Go 时 syn 恒为 0，即使命中源码。
- **kind 例**（`ClaimArenaReward` + `--lang all`，max-anchors 默认 12 时抽样）：source / protocol / config / docs / consumer；非 Go source **无** syntax 字段。
- **失败**：本轮脚本跑通，无超时；`invalid_lang` → `invalid_request` exit 2（预期）。xlsx `latest.*` 未改动。

## 脚本

- `bench/scripts/bench_lang_rg_vs_codefind.py`
- `bench/scripts/bench_lang_rg_vs_codefind.sh`

## 产出

- `bench/results/lang_latest.json` / `lang_latest.csv` / `lang_summary.md`

