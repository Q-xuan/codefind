package main

const searchHelpNotes = `
补充：
  --path 可以是 root 内相对目录或单个文件，可重复。! 前缀排除该路径。
  xlsx 目录冷启动：按文件名/表名/共享字符串出现次数排序，并列不取最小文件。
  只扫一簿。看 score/size 与 next_path；--path 点名后仍保留 deferred 队列。
  --timeout 上限仍是 10s；20s 会 invalid_request。不要加超时，也不要近义匹配。
  零字面命中是 unknown，不能写成「表里没有」或「功能不存在」。
  read 的 --field 与 --range 见 --help-xlsx 或 codefind read --help。
`

const xlsxHelpText = `codefind XLSX 搜索与 read

搜索（--format xlsx）：
  不知道文件名时，直接对目录跑一次。廉价发现按文件名、表名、共享字符串
  字面量出现次数排序；并列时中等大小（约 0.5–6MiB）优先，不取最小文件。
  只内容扫描一簿。files[].score / size 是排名依据；next_path 与
  reason=deferred 是下一跳。--path 点名单簿仍会列出同目录其余 deferred。
  点名多簿会全部内容扫描。不要先 !排除大表再扫剩余目录。
  --timeout 上限仍是 10s；不要把 20s 当成合法值。不做近义自动命中。

read --range 与 --field：
  --range A44:L55
      按已知坐标回读矩形。范围已知时优先用它。
  --anchor AL6 --field 类型
      按表头名猜列。邻近同名表头（多个「类型」）可能选错列。
      范围已知时，--range 比 --field 更稳，不要用 --field 碰运气。

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
  零字面命中或读空不是「表里没有」；只表示当前范围/策略下 unknown。
`
