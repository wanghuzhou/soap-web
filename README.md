# soap-web

SOAP / WSDL 接口调试工具 —— 以 Go 为核心的现代 Web 版 SoapUI 替代方案。

解决 SoapUI 的老问题：界面老旧、体积臃肿（需 JVM）、高分屏（1080p/4K）下渲染模糊。
soap-web 是一个**单二进制、免安装、Web 界面、矢量渲染任意分辨率清晰**的轻量替代。

## 特性

- **导入 SoapUI 工程**：解析 `.xml` 工程，载入 service / operation / 测试用例步骤及其已保存的请求报文与 endpoint，可一键重发。
- **WSDL 解析**：解析 WSDL 1.1 + 内嵌 XSD，列出所有 operation，并自动生成结构正确的请求模板（支持嵌套 complexType、extension 继承、choice 降级）。
- **SOAP 请求发送**：支持 SOAP 1.1（`text/xml` + `SOAPAction`）与 SOAP 1.2（`application/soap+xml` + action）。
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
| POST | `/api/import/soapui` | 上传 SoapUI 工程 xml（multipart `file` 或原始 body） |
| POST | `/api/wsdl/parse` | 上传 WSDL（multipart `file` 或原始 body），返回 operation 与请求模板 |
| GET | `/api/health` | 健康检查 |
