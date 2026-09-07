# codefind vs rg：游戏数值表 XLSX 基准对比

- 夹具：`bench/fixtures/game_tables/survivor_game_tables.xlsx`
- 构建：codefind `0.2.0-rc.1` / git `7de8eb1` — feat(search): 增加游戏项目多语言源码搜索
- XLSX：17 个 sheet（heroLevel/skill/monster/prop/weapon/suit/map…），含中文玩法名与配置 ID
- 方法：
  - **rg_raw**：对原始 `.xlsx`（zip 二进制）单次 `rg -a -F`
  - **rg_unzip**：解压后对 XML 单次 `rg -a -F`（代理常见临时方案）；hits 用 `--count-matches`
  - **codefind_xlsx**：`codefind --format xlsx --term`（原生读单元格，**不调用 rg**）
- 每查询跑 3 次取 **median** 墙钟时间（ms）

## 结果表（median）

| 查询 | 极性 | rg_raw ms / hits / cell / json | rg_unzip ms / hits / noise / cell / json | codefind ms / anchors / raw / cell / json / status |
|---|---|---|---|---|
| `勇士匕首` | pos | 2 / 0 / 0 / 0 | 5 / 17 / 17 / 0 / 0 | 35 / 17 / 17 / 1 / 1 / `candidates_found` |
| `如来神掌` | pos | 2 / 0 / 0 / 0 | 4 / 12 / 12 / 0 / 0 | 38 / 12 / 12 / 1 / 1 / `candidates_found` |
| `电锯人` | pos | 2 / 0 / 0 / 0 | 4 / 2 / 2 / 0 / 0 | 36 / 2 / 2 / 1 / 1 / `candidates_found` |
| `甲贺忍法帖` | pos | 2 / 0 / 0 / 0 | 4 / 5 / 5 / 0 / 0 | 35 / 5 / 5 / 1 / 1 / `candidates_found` |
| `energyMax` | pos | 2 / 0 / 0 / 0 | 5 / 1 / 1 / 0 / 0 | 37 / 1 / 1 / 1 / 1 / `candidates_found` |
| `牛仔套装` | pos | 2 / 0 / 0 / 0 | 5 / 1 / 1 / 0 / 0 | 35 / 1 / 1 / 1 / 1 / `candidates_found` |
| `雷霆之拳` | pos | 2 / 0 / 0 / 0 | 4 / 11 / 11 / 0 / 0 | 36 / 11 / 11 / 1 / 1 / `candidates_found` |
| `城市街道` | pos | 2 / 0 / 0 / 0 | 4 / 1 / 1 / 0 / 0 | 36 / 1 / 1 / 1 / 1 / `candidates_found` |
| `挑战券` | neg | 2 / 0 / 0 / 0 | 5 / 0 / 0 / 0 / 0 | 36 / 0 / 0 / 0 / 1 / `no_candidates` |
| `PvpArena999` | neg | 2 / 0 / 0 / 0 | 5 / 0 / 0 / 0 / 0 | 37 / 0 / 0 / 0 / 1 / `no_candidates` |

## 结论（产品真相）

1. **rg 不是为 XLSX 设计的**：raw 模式对压缩二进制几乎得不到可用命中；unzip 后能在 sheet XML 里命中字面量，但输出是碎 XML，**没有 sheet 名 + A1 坐标的结构化字段**，命中行几乎全是标签噪声。
2. **codefind xlsx 模式不走 rg**（metrics.rg_calls=0）：直接解析工作簿单元格（及传统批注），返回带 `path` / `workbook.sheet` / `workbook.cell` 的 agent 可用 JSON，并有 timeout / max-matches / max-anchors 预算。
3. **优势不在“比 rg 更快地 grep 文本”**：本基准里 codefind 墙钟常约 30–40ms，rg_unzip 约 4–6ms；优势是**结构化候选 + 预算控制 + sheet/cell 定位**，降低 agent 二次解析与误读成本。
4. **负例**（挑战券 / PvpArena999）：rg 与 codefind 均为 0 命中 / `no_candidates`，且**不能据此断言功能不存在**。
5. **text 模式**最多两次 bounded rg；本文件对比的是 **xlsx 模式 vs agent 用 rg 硬搜表** 的现实路径。
