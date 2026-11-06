# soap-web

SOAP / WSDL 接口调试工具 —— 以 Go 为核心的现代 Web 版 SoapUI 替代方案。

解决 SoapUI 的老问题：界面老旧、体积臃肿（需 JVM）、高分屏（1080p/4K）下渲染模糊。
soap-web 是一个**单二进制、免安装、Web 界面、矢量渲染任意分辨率清晰**的轻量替代。

## 特性

- **工程持久化**：导入的 SoapUI 工程、解析的 WSDL 会保存在本地 `data/projects.json`，重启/刷新不丢失。
- **完整增删改**：工程 / 服务(service) / 操作(operation) / 请求样例(call) 均可新建、重命名、删除；删除有二次确认。
- **工程级代理**：每个工程可单独设置 **系统代理 / 直连 / 自定义代理**；自定义支持 **HTTP** 与 **SOCKS5**，用户名密码可留空。用于内网服务无法直连的场景。
- **工程重命名 & 导出**：可重命名工程；可导出为 SoapUI 兼容的 `.xml`，方便带回 SoapUI 或分享给同事（导出的工程可再次被本工具导入，结构一致）。
- **导入 SoapUI 工程**：解析 `.xml` 工程，载入 service / operation / 测试用例步骤及其已保存的请求报文与 endpoint，可一键重发。
- **WSDL 解析**：解析 WSDL 1.1 + 内嵌 XSD，列出所有 operation，并为每个 operation 自动生成一条结构正确的请求样例（支持嵌套 complexType、extension 继承、choice 降级）。
- **WSDL URL 导入**：除本地文件外，可直接填写 `http://host/service?wsdl` 由后端抓取并解析，适用于只有内网可达的服务；下载支持**直连（默认）/ 系统代理 / 自定义 HTTP·SOCKS5 代理**，也可复用已有工程的代理配置；支持 GBK 等非 UTF-8 编码，并对协议（仅 http/https）、大小（8 MB）与超时（30s）做限制。
- **SOAP 请求发送**：支持 SOAP 1.1（`text/xml` + `SOAPAction`）与 SOAP 1.2（`application/soap+xml` + action）。
- **请求体编辑**：XML 语法高亮、格式化/压缩、长行自动换行。
- **响应多视图**：格式化（语法高亮）/ 原始 / Headers，显示状态码、耗时、大小，支持一键复制。
- **非 UTF-8 编码支持**：GBK / GB2312 / GB18030 / Big5 的响应体与 WSDL、SoapUI 工程均可正确解码（国内老 SOAP 服务常见）。响应体大小上限 16 MB，超出会标记「已截断」。
- **报文保真**：格式化与换行规整不会改写文本内容——`<Token>  abc  </Token>` 的前后空格、`xml:space="preserve"`、多行 base64、含 `\n` 的 JSON/正则/Windows 路径都会原样保留。
- **并发安全**：工程读写返回值均为深拷贝，写操作走「克隆 → 落盘 → 换入」，磁盘失败时内存状态不变；改代理走窄接口，不会用旧快照覆盖其他标签页的改动。发送带请求令牌，切换请求样例后迟到的响应会被丢弃，并支持中途取消。
- **界面优化**：支持白天/夜间主题切换（跟随系统，记忆选择）；左中右三栏可拖动调整宽度（记忆宽度，窗口缩放自动收敛）；窄屏自动切换为请求/响应上下堆叠；长标题自动省略不撑高行；顶栏「本地服务状态」每 10 秒探活 `/api/health`，后端停止后自动变红提示“已断开”，点击可立即重试。
- **可维护前端**：前端按职责拆分为 ES Modules（state / api / xml / tree / editor / response / proxy / wsdlurl / theme / layout），无构建步骤、无外部依赖，仍随 Go 二进制内嵌分发。

> 范围说明：本项目**只做**"导入 + WSDL 生成请求模板 + 发送测试"。
> 不包含断言引擎、Groovy 脚本、WS-Security —— 这些是 SoapUI 兼容的深水区，明确不在范围内。

## 运行

```bash
# 开发运行（显式指定数据文件，避免 go run 的临时可执行目录）
go run ./cmd/soapweb -data data/projects.json

# 构建单文件（与启动脚本一致）
go build -o bin\soapweb.exe .\cmd\soapweb
.\bin\soapweb.exe -addr 127.0.0.1:8420
```

程序会尝试自动打开默认浏览器；如未打开，请手动访问 http://127.0.0.1:8420

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
internal/api        REST API（路由按 method + path wildcard 注册）
web/assets/index.html  页面骨架
web/assets/style.css   设计变量 / 三栏布局 / 响应式样式
web/assets/js/         前端 ES Modules（无构建步骤）
  app.js               入口与模块装配
  state.js             共享状态与树查找
  api.js               API 客户端
  xml.js               XML 格式化 / 压缩 / 语法高亮 / 换行规整
  tree.js              工程树渲染与 CRUD
  editor.js            请求编辑与发送
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
```

`xml.selftest.mjs` 直接 import 真实的 ES Modules，覆盖格式化语义保真、换行规整不误伤数据、语法高亮转义等不变量。
`tree.selftest.mjs` 用最小 DOM 桩跑真实的 `tree.js`，断言渲染契约：**结构变更必须 `renderTree()`**、**纯选中变更只能 `syncSelection()`**——两者混用会导致「新建请求后不刷新页面就看不到」或「每次点击都重建整棵树」。
两个脚本失败时都以非零码退出，可挂到 CI。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/send` | 发送 HTTP/SOAP 请求，返回状态、头、体、耗时 |
| POST | `/api/import/soapui` | 上传 SoapUI 工程 xml，解析并**保存**，返回保存后的工程 |
| POST | `/api/wsdl/parse` | 上传 WSDL（或 JSON `{url}`）解析并**保存**为新工程，返回 `{service, project}` |
| POST | `/api/wsdl/fetch` | 从 URL 抓取 WSDL（JSON `{url, proxy?, projectId?}`，默认直连，可指定代理或复用工程代理）并**保存**为新工程 |
| GET | `/api/projects` | 列出所有已保存工程 |
| POST | `/api/projects` | 新建工程 |
| GET/PUT/DELETE | `/api/projects/{pid}` | 获取 / 整体更新（含重命名）/ 删除工程 |
| PUT | `/api/projects/{pid}/proxy` | 仅更新工程代理设置（避免整体覆盖并发修改） |
| GET | `/api/projects/{pid}/export` | 导出为 SoapUI 兼容 XML（下载） |
| POST | `/api/projects/{pid}/services` | 新建服务 |
| DELETE | `/api/projects/{pid}/services/{sid}` | 删除服务 |
| POST | `/api/projects/{pid}/services/{sid}/operations` | 新建操作 |
| DELETE | `/api/projects/{pid}/services/{sid}/operations/{oid}` | 删除操作 |
| POST | `/api/projects/{pid}/services/{sid}/operations/{oid}/calls` | 新建请求样例 |
| PUT/DELETE | `.../operations/{oid}/calls/{cid}` | 更新 / 删除请求样例 |
| GET | `/api/health` | 健康检查 |

## 数据存储

默认数据文件位于可执行文件旁的 `data/projects.json`（可用 `-data <路径>` 覆盖）。
写入采用临时文件 + 原子 rename，避免崩溃导致文件损坏。
