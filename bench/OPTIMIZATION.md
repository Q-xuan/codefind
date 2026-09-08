# 优化交接：基于 bench 基线的产品缺口

本文档供审阅本 PR / 后续改动的同学（含其他 AI）使用。基线结果是**诚实的弱结果**：若只按墙钟速度或原始命中行数评判，codefind **很难宣称全面碾压 rg**。请把本目录当作对照材料，而不是营销材料。

详细数字见：

- XLSX：`bench/results/summary.md`
- `--lang`：`bench/results/lang_summary.md`

---

## 基线在说什么

### 1. XLSX：速度不是赢面

| 现象 | 数量级（median） |
|---|---|
| `codefind --format xlsx` | 约 **35–38 ms** |
| `rg_unzip`（解压后单次 `rg -a -F`） | 约 **4–5 ms** |
| `rg_raw`（对 zip 二进制） | 极快但 **几乎无可用命中** |

产品真相：

- **赢的是结构化输出**：`path` + `workbook.sheet` + `workbook.cell`（A1）的 agent 可用 JSON；`rg_unzip` 命中的是碎 XML，噪声高、无 sheet/cell 合同。
- **xlsx 模式不走 rg**（`metrics.rg_calls=0`），走原生读表路径。
- 若 KPI 只看 wall clock 或 raw hit count，本基线会显示 codefind **更慢**；请用「agent 能否直接定位单元格、是否需要二次解析」等指标。

### 2. `--lang`：默认 Go 会漏非 Go 线索

| 现象 | 含义 |
|---|---|
| 不传 `--lang` → `query.languages=["go"]` | 默认只收窄到 Go（域文件 proto/md/csv/yaml 仍可搜） |
| `LUA_ONLY_*` / `TS_ONLY_*` 在 default 下 | **`no_candidates` / 0 源码命中** — 必须 opt-in `--lang lua` / `ts` / `all` |
| naive 单次 `rg` | 往往命中更多字面量，但夹带 **vendor / node_modules / \*.min.js** 噪声 |
| 非 Go 命中 | 仅有词法候选（source/config 等），**无 AST `syntax`**；AST 仍基本限于 Go |

产品真相：`--lang` 是显式能力开关，不是「默认全语言智能」。文档/UX 若不说清，agent 会把 default miss 误读成「功能不存在」。

### 3. 合同约束（优化时不可破坏）

请在**不破坏现有对外合同**的前提下提优化方案：

1. **预算字段保留**：timeout / max-matches / max-anchors / limits / metrics 语义稳定。
2. **text 模式 `rg_calls` ≤ 2**（bounded）。
3. **xlsx 走原生路径**（不把 xlsx 退化成「再调两次 rg」）。
4. **不做代码图谱 / 全库索引类膨胀**（保持轻量候选工具定位）。
5. **`zero ≠ absent`**：`no_candidates` / 0 命中只表示 unknown，不能断言功能不存在。
6. **非 Go 维持词法候选**（可增强投影/排序/过滤，但不假装已有全语言 AST，除非另开明确能力面）。

---

## 请审阅者提议的优化方向（假设，非处方）

在遵守上一节合同的前提下，欢迎提出具体改动（含预期指标与回归方式）。可选焦点：

1. **xlsx 解析延迟**：在保持 sheet/cell JSON 合同下压低 35–38ms（解析策略、复用、IO、投影成本等）。
2. **面向 agent 的「有用性」指标**：超越 wall clock — 例如「是否含可用 sheet/cell」「二次解析成本」「噪声率」「预算是否可见」；可扩展基准脚本，但勿只刷速度榜。
3. **`--lang` UX / 文档**：默认 Go miss 的可发现性；何时建议 `--lang all`；invalid lang 行为已清晰（`invalid_request`），可加强帮助文案。
4. **投影质量**：anchors 排序、kind、去噪（vendor/min）、max-anchors 下的代表性；让「命中更少但更可行动」在指标上可见。
5. **多语言词法路径**：在无 AST 前提下提升非 Go 线索的召回/可解释性（仍须 opt-in 语义清晰）。

---

## 复现

见同目录 `README.md`。请基于本分支 `bench/` 夹具复跑后再改产品代码，避免只改文档数字。

