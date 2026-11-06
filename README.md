# soap-web

SOAP / WSDL 接口调试工具 —— 以 Go 为核心的现代 Web 版 SoapUI 替代方案。

解决 SoapUI 的老问题：界面老旧、体积臃肿（需 JVM）、高分屏（1080p/4K）下渲染模糊。
soap-web 是一个**单二进制、免安装、Web 界面、矢量渲染任意分辨率清晰**的轻量替代。

## 特性

- **工程持久化**：导入的 SoapUI 工程、解析的 WSDL 会保存在本地 `data/projects.json`，重启/刷新不丢失。
- **完整增删改**：工程 / 服务(service) / 操作(operation) / 请求样例(call) 均可新建、重命名、删除；删除有二次确认。
- **工程重命名 & 导出**：可重命名工程；可导出为 SoapUI 兼容的 `.xml`，方便带回 SoapUI 或分享给同事（导出的工程可再次被本工具导入，结构一致）。
- **导入 SoapUI 工程**：解析 `.xml` 工程，载入 service / operation / 测试用例步骤及其已保存的请求报文与 endpoint，可一键重发。
- **WSDL 解析**：解析 WSDL 1.1 + 内嵌 XSD，列出所有 operation，并自动生成结构正确的请求模板（支持嵌套 complexType、extension 继承、choice 降级）。
- **SOAP 请求发送**：支持 SOAP 1.1（`text/xml` + `SOAPAction`）与 SOAP 1.2（`application/soap+xml` + action）。
- **请求体编辑**：XML 语法高亮、格式化/压缩、长行自动换行。
- **响应多视图**：格式化（语法高亮）/ 原始 / Headers，显示状态码、耗时、大小。

> 范围说明：本项目**只做**"导入 + WSDL 生成请求模板 + 发送测试"。
> 不包含断言引擎、Groovy 脚本、WS-Security —— 这些是 SoapUI 兼容的深水区，明确不在范围内。

## 运行

```bash
# 开发运行
go run ./cmd/soapweb

# 构建单文件
go build -o soapweb.exe ./cmd/soapweb
./soapweb.exe -addr 127.0.0.1:8420
```

然后浏览器打开 http://127.0.0.1:8420

## 目录结构

```
cmd/soapweb         入口，启动 HTTP 服务
internal/model      共享数据结构
internal/transport  HTTP/SOAP 发送与响应归一化
internal/soapuiproj SoapUI project xml 解析
internal/wsdl       WSDL 1.1 + XSD 解析与请求模板生成
internal/api        REST API（/api/send, /api/import/soapui, /api/wsdl/parse）
web/                内嵌前端（embed.FS，零依赖 SPA）
```

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/send` | 发送 HTTP/SOAP 请求，返回状态、头、体、耗时 |
| POST | `/api/import/soapui` | 上传 SoapUI 工程 xml，解析并**保存**，返回保存后的工程 |
| POST | `/api/wsdl/parse` | 上传 WSDL，解析并**保存**为新工程，返回 `{service, project}` |
| GET | `/api/projects` | 列出所有已保存工程 |
| POST | `/api/projects` | 新建工程 |
| GET/PUT/DELETE | `/api/projects/{pid}` | 获取 / 整体更新（含重命名）/ 删除工程 |
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
