# AutoProxy3

AutoProxy3 是一个使用 Go 实现的代理与规则编排实验项目。当前代码已经具备配置加载、规则源抽象、管理接口处理器、重载客户端、代理判路与 auto-detect 记录链路等核心部件；`serve` 命令仍是最小入口，`Runtime.Run(ctx)` 目前只做上下文检查，不会在当前版本中长期托管代理端口或管理 HTTP 监听。

## 能力概览

- 通过 `config.json` 加载代理、规则源、管理接口与日志配置。
- 支持在线规则源 `web_rules`、本地规则源 `custom_rules` 与 `auto_detect` 结果持久化文件。
- 提供管理接口处理器：`GET /`、`POST /reload_web_rules`、`POST /reload_custom_rules`、`POST /reload_rules`。
- 提供 CLI 重载命令：`reload_web_rules`、`reload_custom_rules`、`reload_rules`，默认访问 `http://127.0.0.1:<listen_port>`。
- 代理层已实现普通 HTTP 请求与 CONNECT 隧道的判路、上游转发与 auto-detect 结果记录。

## 快速开始

### 环境要求

- Go 1.24

### 准备配置

```bash
cp configs/config.json.example config.json
```

按需修改字段后，可查看版本或帮助：

```bash
go run ./src/cmd/autoproxy3 version
go run ./src/cmd/autoproxy3 help
```

### 运行说明

```bash
go run ./src/cmd/autoproxy3 --config config.json serve
```

当前 `serve` 命令会完成配置解析、运行时装配与上下文检查，但不会在当前版本中启动长生命周期的代理监听或管理监听。因此，README 中提到的管理接口属于当前代码已经实现的处理器契约，而不是已经由 `serve` 自动对外提供的完整运行形态。

## 配置方式

完整示例见 `configs/config.json.example`，顶层字段如下：

- `listen_addr`
- `upstream_proxy`
- `web_rules`
- `custom_rules`
- `auto_detect`
- `management`
- `logging`

路径字段会在加载配置时以配置文件所在目录为基准解析为绝对路径，涉及：

- `web_rules.cache_path`
- `custom_rules.path`
- `auto_detect.rules_path`
- `logging.file_path`

更多说明见 `docs/guides/configuration.md`。

## 命令说明

### `serve`

```bash
go run ./src/cmd/autoproxy3 --config config.json serve
```

装配运行时并调用 `Runtime.Run(ctx)`。当前实现只校验上下文是否已取消。

### `version`

```bash
go run ./src/cmd/autoproxy3 version
```

输出当前版本号。仓库默认版本定义在 `src/internal/buildinfo/buildinfo.go`，当前默认值为 `0.1.0`。

### `help`

```bash
go run ./src/cmd/autoproxy3 help
```

输出命令帮助与默认配置路径 `config.json`。

### `reload_web_rules`

```bash
go run ./src/cmd/autoproxy3 --config config.json reload_web_rules
```

通过本地 management client 向 `POST /reload_web_rules` 发送请求。

### `reload_custom_rules`

```bash
go run ./src/cmd/autoproxy3 --config config.json reload_custom_rules
```

通过本地 management client 向 `POST /reload_custom_rules` 发送请求。当前语义为统一处理本地 `custom_rules` 与 `auto_detect` 规则重载。

### `reload_rules`

```bash
go run ./src/cmd/autoproxy3 --config config.json reload_rules
```

通过本地 management client 依次触发在线规则与本地规则重载。

## 管理接口入口

管理接口配置位于 `management.listen_port`，默认值为 `9091`。CLI reload 命令固定访问：

```text
http://127.0.0.1:<management.listen_port>
```

当前代码已经实现以下 HTTP 处理器契约：

- `GET /`
- `POST /reload_web_rules`
- `POST /reload_custom_rules`
- `POST /reload_rules`

接口详情与响应示例见 `docs/api/management.md`。

## 测试命令

```bash
go test ./...
go test ./... -cover
```

如需查看更详细覆盖率，可使用：

```bash
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
```

## 文档索引

- 运行时架构：`docs/architecture/runtime.md`
- 管理接口：`docs/api/management.md`
- 开发流程：`docs/workflows/development.md`
- 配置指南：`docs/guides/configuration.md`
