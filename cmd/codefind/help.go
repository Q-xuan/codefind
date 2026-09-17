package main

const searchHelpNotes = `
补充：
  --path 可以是 root 内相对目录或单个文件，可重复。! 前缀排除该路径。
  xlsx 模式下可点名或排除单个 .xlsx，避免 7–9MB 大表吃满整次 timeout。
  --timeout 上限仍是 10s；20s 会 invalid_request。大表先过滤，不要先加超时。
  零字面命中是 unknown，不能写成「表里没有」或「功能不存在」。
  read 的 --field 与 --range 见 --help-xlsx 或 codefind read --help。
`

const xlsxHelpText = `codefind XLSX 搜索与 read

搜索（--format xlsx）：
  --path 可以是目录，也可以点名单个 .xlsx。
  --path '!合十_关卡数值.xlsx' 从目录枚举里排除该簿（可重复）。
  只有排除项时，默认仍扫授权根下的 '.'。
  7–9MB 工作簿会吃满单次 timeout。先点名目标簿或排除大簿。
  --timeout 上限仍是 10s；不要把 20s 当成合法值。

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
