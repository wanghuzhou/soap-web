# soap-web

SOAP / WSDL 接口调试工具 —— 以 Go 为核心的现代 Web 版 SoapUI 替代方案。

解决 SoapUI 的老问题：界面老旧、体积臃肿（需 JVM）、高分屏（1080p/4K）下渲染模糊。
soap-web 是一个**单二进制、免安装、Web 界面、矢量渲染任意分辨率清晰**的轻量替代。

## 特性

- **工作空间**：工程按工作空间隔离，每个工作空间一个独立的 JSON 文件。顶栏下拉即可新建 / 切换 / 重命名 / 删除，切换无需重启；界面只显示工作空间名字，文件路径是内部细节。`settings.json` 记录当前空间与「名字 → 文件」映射，重启后回到上次使用的空间。
- **工作空间导入/导出**：可把当前工作空间导出为 JSON 文件，导出时可选是否保留工程代理配置（默认不保留，避免把代理密码带到别的机器）；导入 JSON 会新建一个工作空间并切换过去。
- **工程持久化**：导入的 SoapUI 工程、解析的 WSDL 会保存在当前工作空间的 JSON 文件，重启/刷新不丢失。
- **完整增删改**：工程 / 服务(service) / 操作(operation) / 请求样例(call) 均可新建、重命名、删除；删除有二次确认。
- **工程级代理**：每个工程可单独设置 **系统代理 / 直连 / 自定义代理**；自定义支持 **HTTP** 与 **SOCKS5**，用户名密码可留空。用于内网服务无法直连的场景。
- **工程重命名 & 导出**：可重命名工程；可导出为 SoapUI 兼容的 `.xml`，方便带回 SoapUI 或分享给同事（导出的工程可再次被本工具导入，结构一致）。
- **导入 SoapUI 工程**：解析 `.xml` 工程，载入 service / operation / 测试用例步骤及其已保存的请求报文与 endpoint，可一键重发。
- **WSDL 解析**：解析 WSDL 1.1 + 内嵌 XSD，列出所有 operation，并为每个 operation 自动生成一条结构正确的请求样例（支持嵌套 complexType、extension 继承、choice 降级）。
- **WSDL URL 导入**：除本地文件外，可直接填写 `http://host/service?wsdl` 由后端抓取并解析，适用于只有内网可达的服务；下载支持**直连（默认）/ 系统代理 / 自定义 HTTP·SOCKS5 代理**，也可复用已有工程的代理配置；支持 GBK 等非 UTF-8 编码，并对协议（仅 http/https）、大小（8 MB）与超时（30s）做限制。
- **SOAP 请求发送**：支持 SOAP 1.1（`text/xml` + `SOAPAction`）与 SOAP 1.2（`application/soap+xml` + action）。
- **自定义请求头**：请求面板里的「请求头」可添加任意头（Authorization、token、appKey 等）。支持两层：**工程默认**（对工程内所有请求生效）+ **样例覆盖**（只改当前请求样例）。同名时样例优先，比较不区分大小写。`Authorization`、`Cookie`、含 token/secret/apikey 的头默认以密码框遮蔽，可勾选底部的「遮蔽敏感头」关闭。工程默认头在样例里只读，填写同名的行即可覆盖。
- **超时设置**：Endpoint 旁可填毫秒值，留空按 60 秒；范围 1 秒～10 分钟。超时随请求样例保存。
- **复制请求样例**：请求行上的 ⧉ 按钮复制整条样例（报文、Endpoint、SOAPAction、请求头、超时），标题自动加「(副本)」并避开重名。
- **树搜索**：工程树顶部输入框按名字过滤工程/服务/操作/请求，命中行及其祖先保持可见，命中项高亮；过滤不重建 DOM，焦点与滚动位置不丢；`Esc` 清空。
- **复制为 curl**：请求编辑区「复制为 curl」把当前请求导出成 bash/zsh 可执行的命令（`--request` / `--header` / `--data-raw`，反斜杠续行），便于在服务器上复现；不写 `--max-time`，复现时可随时中断。命令里含敏感头时会先确认，报文超过 512 KB 会提示不适合转命令行。
- **请求体编辑**：XML 语法高亮、格式化/压缩、长行自动换行。
- **响应多视图**：格式化（语法高亮）/ 原始 / Headers，显示状态码、耗时、大小，支持一键复制。
- **非 UTF-8 编码支持**：GBK / GB2312 / GB18030 / Big5 的响应体与 WSDL、SoapUI 工程均可正确解码（国内老 SOAP 服务常见）。响应体大小上限 16 MB，超出会标记「已截断」。
- **报文保真**：格式化与换行规整不会改写文本内容——`<Token>  abc  </Token>` 的前后空格、`xml:space="preserve"`、多行 base64、含 `\n` 的 JSON/正则/Windows 路径都会原样保留。
- **并发安全**：工程读写返回值均为深拷贝，写操作走「克隆 → 落盘 → 换入」，磁盘失败时内存状态不变；改代理走窄接口，不会用旧快照覆盖其他标签页的改动。发送带请求令牌，切换请求样例后迟到的响应会被丢弃，并支持中途取消。
- **多标签页工作空间保护**：每个写请求携带当前工作空间 id；若另一个标签页切换了工作空间，旧标签页的写请求返回 409 而不是写进错误的空间。顶栏每 10 秒心跳也会发现外部切换，无未保存修改时自动同步，有修改时先提示。
- **界面优化**：支持白天/夜间主题切换（跟随系统，记忆选择）；左中右三栏可拖动调整宽度（记忆宽度，窗口缩放自动收敛）；窄屏自动切换为请求/响应上下堆叠；长标题自动省略不撑高行；顶栏「本地服务状态」每 10 秒探活 `/api/health`，后端停止后自动变红提示“已断开”，点击可立即重试。
- **可维护前端**：前端按职责拆分为 ES Modules（state / api / workspace / popover / xml / tree / editor / response / proxy / wsdlurl / theme / layout），无构建步骤、无外部依赖，仍随 Go 二进制内嵌分发。

> 范围说明：本项目**只做**"导入 + WSDL 生成请求模板 + 发送测试"。
> 不包含断言引擎、Groovy 脚本、WS-Security —— 这些是 SoapUI 兼容的深水区，明确不在范围内。

## 运行

```bash
# 开发运行（显式指定数据目录，避免 go run 的临时可执行目录）
go run ./cmd/soapweb -data data

# 构建单文件（与启动脚本一致）
go build -o bin\soapweb.exe .\cmd\soapweb
.\bin\soapweb.exe -addr 127.0.0.1:8420
```

程序会尝试自动打开默认浏览器；如未打开，请手动访问 http://127.0.0.1:8420

### 桌面壳 POC（Windows）

在原生窗口里运行同一套后端与前端（Wails v2 + WebView2），不监听端口：

```bash
go build -tags production -ldflags "-H windowsgui" -o bin\soapweb-desktop.exe .\cmd\soapweb-desktop
.\bin\soapweb-desktop.exe
```

- 数据目录默认与浏览器版一致（可执行文件旁 `data`），也可用 `-data` 指定；WebView2 配置存放在数据目录下的 `webview2/`。
- 需要系统已安装 WebView2 Runtime（Windows 10/11 通常自带）。
- 这是 POC：暂未包含单实例锁、菜单/托盘、代码签名；浏览器入口 `cmd/soapweb` 不受影响。

### 桌面壳 POC（macOS）

在原生窗口里运行同一套后端与前端（Wails v2 + WKWebView），不监听端口：

```bash
./build-macos.sh      # 产物 bin/soap-web.app
open bin/soap-web.app
```

- 需要 Xcode Command Line Tools（`xcode-select --install`）。Wails 的 macOS 后端是 cgo + Objective-C，要链接 Cocoa/WebKit，**无法在 Linux 上交叉编译**（`CGO_ENABLED=0` 会报 `undefined: Frontend`，`CGO_ENABLED=1` 会找不到 darwin 的 clang）。
- 必须带 `-tags production`。漏掉 tag 的二进制能启动，但会立刻打印 `Wails applications will not build without the correct build tags.` 后退出。
- 数据目录默认在 `~/Library/Application Support/soap-web/data`：`.app` 包里的 `Contents/` 是被代码签名封印的资源，往里写数据会让签名失效（`codesign --verify` 报 `a sealed resource is missing or invalid`）。也可用 `-data` 指定其他目录。
- `build-macos.sh` 用的是 ad-hoc 签名，够本机自测；分发给别人需要 Developer ID 证书 + 公证，否则首次打开会被 Gatekeeper 拦下。

### Windows 提示“未知发布者”

`go build` 生成的是未签名可执行文件，Windows SmartScreen / UAC 可能显示“未知发布者”。这是系统对未签名程序的正常提示，不代表程序损坏或有病毒：

1. 右键 `bin\soapweb.exe` → 属性 → 常规；只有从网络/压缩包下载的文件才会出现“解除锁定”，本地构建的文件没有该选项是正常的，可跳过；
2. 启动时如出现 SmartScreen，选择“更多信息 → 仍要运行”；
3. 也可直接使用 `启动 soap-web.bat`，预编译程序无法启动时会自动回退到 `go run`（需本机安装 Go）。

如需正式分发给其他电脑，建议使用代码签名证书对 `bin\soapweb.exe` 签名，避免“未知发布者”提示。

如果控制台提示 `127.0.0.1:8420` 端口被占用，说明已有一个 soap-web 实例在运行；关闭旧窗口，或在任务管理器结束旧的 `soapweb.exe` 后再启动。

## 目录结构

```
cmd/soapweb         入口，启动 HTTP 服务
internal/model      共享数据结构
internal/charset    GBK / GB2312 / GB18030 / Big5 解码（响应体与 XML 文档共用）
internal/transport  HTTP/SOAP 发送与响应归一化
internal/soapuiproj SoapUI project xml 解析与导出
internal/wsdl       WSDL 1.1 + XSD 解析与请求模板生成
internal/wsdlfetch  从 URL 抓取 WSDL（协议/大小/超时限制，走工程代理）
internal/store      工程数据持久化（JSON 原子写入）
internal/workspace  工作空间注册表（settings.json / 切换 / 迁移 / 软删除）
internal/fileutil   原子 JSON 写入与 UTF-8 BOM 处理（store/workspace 共用）
internal/api        REST API（路由按 method + path wildcard 注册）
web/assets/index.html  页面骨架
web/assets/style.css   设计变量 / 三栏布局 / 响应式样式
web/assets/js/         前端 ES Modules（无构建步骤）
  app.js               入口与模块装配
  state.js             共享状态与树查找
  api.js               API 客户端（工作空间请求头 / 409 处理）
  workspace.js         工作空间切换器与增删改
  workspaceio.js       工作空间 JSON 导入/导出
  popover.js           弹出菜单定位（rowmenu/workspace 共用）
  xml.js               XML 格式化 / 压缩 / 语法高亮 / 换行规整
  tree.js              工程树渲染与 CRUD
  tree/render.js       行渲染、折叠可见性、选中标记
  tree/nav.js          ARIA 键盘导航与选中分发
  tree/actions.js      树 CRUD 动作、工程菜单、就地重命名
  tree/search.js       按名字过滤工程树
  editor.js            请求编辑与发送
  editor/textarea.js   编辑区：语法高亮同步与 Tab 缩进
  headers.js           请求头编辑（工程默认 + 样例覆盖，敏感头遮蔽）
  curl.js              生成可执行的 curl 命令
  clipboard.js         剪贴板写入（含 execCommand 降级）
  response.js          响应渲染与复制
  proxy.js             工程代理设置
  wsdlurl.js           从 URL 导入 WSDL 的对话框
  theme.js             明暗主题
  layout.js            可拖拽分栏与宽度持久化
web/selftest/          前端回归自测（不参与 embed，不对外提供）
  xml.selftest.mjs     XML 管线不变量断言
  tree.selftest.mjs    工程树渲染契约断言（DOM 桩）
web/embed.go            embed.FS 内嵌资源
```

## 测试

```bash
go test ./...                              # 后端单元测试
go test -race ./...                        # 含数据竞争检测
node web/selftest/xml.selftest.mjs         # 前端 XML 管线回归自测（需 Node 18+）
node web/selftest/tree.selftest.mjs        # 工程树渲染契约回归自测（需 Node 18+）
node web/selftest/workspace.selftest.mjs   # 工作空间切换器回归自测（需 Node 18+）
node web/selftest/features.selftest.mjs    # 请求头合并 / curl 生成 / 树过滤回归自测
```

`xml.selftest.mjs` 直接 import 真实的 ES Modules，覆盖格式化语义保真、换行规整不误伤数据、语法高亮转义等不变量。
`tree.selftest.mjs` 用最小 DOM 桩跑真实的 `tree.js`，断言渲染契约：**结构变更必须 `renderTree()`**、**纯选中变更只能 `syncSelection()`**——两者混用会导致「新建请求后不刷新页面就看不到」或「每次点击都重建整棵树」。同时覆盖工作空间切换时折叠状态的命名空间隔离与待落盘保存。
`workspace.selftest.mjs` 覆盖工作空间状态同步：名字/标题/请求头一起更新；外部切换且编辑器有未保存修改时只标记过期，绝不静默覆盖。
两个脚本失败时都以非零码退出，可挂到 CI。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/workspaces` | 工作空间列表 + 当前工作空间（只含 id/name） |
| POST | `/api/workspaces` | 新建工作空间并切换（`{name}`） |
| POST | `/api/workspaces/switch` | 切换工作空间（`{id}`） |
| POST | `/api/workspaces/export` | 导出当前工作空间为工程数组（`{keepProxy}`，默认不保留代理） |
| POST | `/api/workspaces/import` | 导入工程数组为新工作空间（可选 `?name=`，默认用文件名） |
| PATCH | `/api/workspaces/{wid}` | 重命名工作空间（`{name}`） |
| DELETE | `/api/workspaces/{wid}` | 删除工作空间（文件移入 `data/trash`；至少保留一个） |
| POST | `/api/send` | 发送 HTTP/SOAP 请求，返回状态、头、体、耗时 |
| POST | `/api/import/soapui` | 上传 SoapUI 工程 xml，解析并**保存**到当前工作空间，返回 `{project, workspaceId}` |
| POST | `/api/wsdl/parse` | 上传 WSDL（或 JSON `{url}`）解析并**保存**为新工程，返回 `{service, project, workspaceId}` |
| POST | `/api/wsdl/fetch` | 从 URL 抓取 WSDL（JSON `{url, proxy?, projectId?}`，默认直连，可指定代理或复用工程代理）并**保存**为新工程，返回 `{service, project, url, workspaceId}` |
| GET | `/api/projects` | 列出所有已保存工程 |
| POST | `/api/projects` | 新建工程 |
| GET/PUT/DELETE | `/api/projects/{pid}` | 获取 / 整体更新（含重命名）/ 删除工程 |
| PUT | `/api/projects/{pid}/proxy` | 仅更新工程代理设置（避免整体覆盖并发修改） |
| PUT | `/api/projects/{pid}/name` | 仅更新工程名称（重命名走窄写，不覆盖并发修改） |
| GET | `/api/projects/{pid}/export` | 导出为 SoapUI 兼容 XML（下载） |
| POST | `/api/projects/{pid}/services` | 新建服务 |
| DELETE | `/api/projects/{pid}/services/{sid}` | 删除服务 |
| POST | `/api/projects/{pid}/services/{sid}/operations` | 新建操作 |
| DELETE | `/api/projects/{pid}/services/{sid}/operations/{oid}` | 删除操作 |
| POST | `/api/projects/{pid}/services/{sid}/operations/{oid}/calls` | 新建请求样例 |
| POST | `.../operations/{oid}/calls/{cid}/duplicate` | 复制请求样例，返回新样例（标题自动加「(副本)」） |
| PUT/DELETE | `.../operations/{oid}/calls/{cid}` | 更新 / 删除请求样例 |
| GET | `/api/health` | 健康检查，同时返回当前工作空间（`{status, workspace}`） |

所有 `/api/*` 请求可带 `X-Workspace-Id` 头。若该 id 与服务端当前工作空间不一致（通常是另一个标签页切换了空间），写操作返回 `409` + `code: "workspace_mismatch"`，前端会提示同步，避免把数据写进错误的空间。

## 数据存储

数据目录默认在可执行文件旁的 `data`，可用 `-data <目录>` 覆盖。旧的 `-data <...>/projects.json` 写法仍然兼容：会以该文件所在目录作为数据目录，并把该文件登记为默认工作空间。

```
data/
  settings.json          # 当前工作空间 + 名字 → 文件映射
  projects.json          # 默认工作空间（首次启动自动登记）
  workspaces/<id>.json   # 之后新建的工作空间
  trash/                 # 删除的工作空间备份
logs/
  soap-web.log           # 运行日志（与 data 同级）
```

### 日志

- 日志文件与数据目录同级：`<数据目录的上一级>/logs/soap-web.log`。默认数据目录是 exe 旁的 `data`，所以日志在 `data` 旁边的 `logs`；用 `-data D:\x\data` 时日志在 `D:\x\logs`。
- 分两级输出：
  - `[调试]` 只写文件，不进控制台。4xx 请求（参数校验失败、过期标签页的 409、文件内容不合法等）属于这一类——排障时要看，但没必要打断用户正在看的窗口。
  - `[错误]` 同时写控制台和文件。5xx、网络/代理失败、启动失败属于这一类。
- 控制台只显示短消息（超过 150 字符截断），文件每行带毫秒时间戳并保留完整内容。
- 记录内容：启动/数据目录初始化、工作空间增删改切换与导入导出、上面两级的错误响应、请求过程中的网络/代理失败，以及耗时超过 1 秒的成功请求。正常的读请求不记录，避免刷屏。
- 文件超过 4MB 时自动改名为 `soap-web-<时间>.log` 并重新开始；日志目录不可写时只输出到控制台，不影响程序启动。

`settings.json` 示例：

```json
{
  "version": 1,
  "current": "40aa591a19894be4",
  "workspaces": [
    { "id": "40aa591a19894be4", "name": "默认", "file": "projects.json" }
  ]
}
```

- 每个工作空间文件就是一个 `Project[]` 数组，格式与旧版 `projects.json` 完全相同，可直接拷贝复用。
- 数据目录或 `settings.json` 不存在时会自动创建（含一个空的 `projects.json`）；`settings.json` 为空时按首次启动处理，不会因为文件缺失而启动失败。
- 默认数据目录不可写时（例如 exe 放在 `Program Files` 或只读目录），会自动改用用户数据目录：Windows 为 `%LOCALAPPDATA%\soap-web\data`，其他系统为 `os.UserConfigDir()/soap-web/data`。显式 `-data` 指定的目录不会回退，失败时会直接报错。
- 首次启动若没有 `settings.json`，会把已有的 `projects.json` 自动登记为「默认」工作空间，不移动、不改名文件。
- `settings.json` / 工作空间文件开头的 UTF-8 BOM 会被忽略（Windows 记事本另存为 UTF-8 时常见）。
- 写入采用临时文件 + 原子 rename，避免崩溃导致文件损坏。切换工作空间时先加载并校验目标文件，失败则保持当前空间与 `settings.json` 不变。
- `settings.json` 本身损坏（JSON 语法错误、路径越界、重名等）时程序会拒绝启动并在控制台指出文件；单个工作空间文件损坏时只在切换到它时报错，当前空间不受影响。
- 删除工作空间不会直接删文件，而是移到 `data/trash/<id>-<时间>.json`；最后一个工作空间不允许删除。需要恢复时，把备份文件移回 `data/workspaces/` 并在 `settings.json` 里补回一条映射即可。
- 导出工作空间默认移除每个工程的 `proxy` 字段；勾选「保留代理配置」才会带出（含明文密码）。
