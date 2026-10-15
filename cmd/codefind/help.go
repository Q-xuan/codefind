package main

const searchHelpNotes = `
补充：
  一次调用只选一种格式。策划 xlsx 用 --format xlsx；源码、协议、csv/tsv、文档用默认 text。
  text：auto 只对非法 UTF-8 的 csv/tsv 再试一次 gb18030，不会整树换编码。看 encoding_applied。
  --path 可以是 root 内相对目录或单个文件，可重复。! 前缀排除该路径。
  xlsx 目录冷启动：按文件名/表名/共享字符串出现次数排序，并列不取最小文件。
  只扫一簿。看 score/size 与 next_path；--path 点名后仍保留 deferred 队列。
  显式批量用 --max-files（1..32），续扫用 --cursor。结果里的 next_command 可直接执行。
  达到批量或时间预算时 content_complete=false，不能当成全目录已扫描。
  --match exact 只命中整格；默认 substring。anchor 带 matched_terms。不做近义。
  --sheet 可只扫指定工作表。超限时看 limit_kind、limit_bytes、actual_bytes、entry、sheet。
  上限：--max-anchors 1..50，--max-cells 1..4096，--max-chars 1..200000，--timeout 10s。
  --timeout 上限仍是 10s；20s 会 invalid_request。不要加超时，也不要近义匹配。
  零字面命中是 unknown，不能写成「表里没有」或「功能不存在」。
  已知格子用 read --range。--field 与 --range 见 --help-xlsx 或 codefind read --help。
`

const xlsxHelpText = `codefind XLSX 搜索与 read

搜索（--format xlsx）：
  不知道文件名时，直接对目录跑一次。廉价发现按文件名、表名、共享字符串
  字面量出现次数排序；并列时中等大小（约 0.5–6MiB）优先，不取最小文件。
  只内容扫描一簿。files[].score / size 是排名依据；next_path 与
  reason=deferred 是下一跳。--path 点名单簿仍会列出同目录其余 deferred。
  点名多簿会全部内容扫描。不要先 !排除大表再扫剩余目录。
  需要一批时用 --max-files。next_command 带 --cursor 续扫，不重复已完成文件。
  失败文件记 read_error，不挡住同批其它文件。未扫完不会声称全目录已扫描。
  --match exact 是整格相等；省略时仍是子串。anchor.matched_terms 是实际命中的词。
  --sheet 只扫点名的表，其它表是 not_requested。超限 sheet 会跳过并给出
  file_size / xml_part / xml_total、阈值、实际大小和 entry，可读的 sheet 仍留下。
  不提高 32MiB / 64MiB 上限。
  --timeout 上限仍是 10s；不要把 20s 当成合法值。不做近义自动命中。
  上限：max-anchors 1..50，max-cells 1..4096，max-chars 1..200000。

read --range 与 --field：
  --range A44:L55
      按已知坐标回读矩形。范围已知时优先用它。
  --anchor AL6 --field 类型
      按表头名猜列。邻近同名表头（多个「类型」）可能选错列。
      范围已知时，--range 比 --field 更稳，不要用 --field 碰运气。

范围续读：
  看 requested_cells、returned_cells、last_cell。
  cell_truncated 是格子数量，char_truncated / text_truncated 是字符预算。
  parse_complete 只表示解析完成，不表示格子已经全部返回。
  resume_cursor 与 next_ranges 覆盖剩余格子，不重叠、不丢格。
  共享公式保留原始 expression。后续格给出 shared_anchor 与 anchor_expression。
  resolved_expression 只是相对引用的机械展开，没有重新计算。
  展不开时 resolve_status=unknown，不静默推算。

零字面命中：
  搜索 0 命中只表示 unknown，不能写成「表里没有该字段」。
  表内常用近义写法（例如「木鱼每日获得功德上限」）不会被「木鱼上限」命中。
  未扫描的大表、批注以外的说明、公式表达式也不在本次证据里。
`

const readHelpNotes = `
read 范围：
  --range A44:L55  已知坐标时使用；只回读该矩形，比 --field 更稳。
  --anchor AL6 --field 类型
      按表头名猜列。邻近同名「类型」列可能选错，范围已知时不要用 --field。
  上限：max-cells 1..4096，max-chars 1..200000。搜索侧 max-anchors 是 1..50。
  格子不够时看 cell_truncated、requested_cells、returned_cells、last_cell。
  字符不够时看 char_truncated 和 text_truncated。两者分开。
  parse_complete 不等于格子已经全部返回。用 resume_cursor 或 next_ranges 续读。
  共享公式的 resolved_expression 只是机械展开，不会重算，cached_value 不能当证明。
  零字面命中或读空不是「表里没有」；只表示当前范围/策略下 unknown。
`
