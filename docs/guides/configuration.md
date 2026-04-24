# 配置指南

## 总览

配置文件由 `src/internal/config/config.go` 解析，默认文件名为 `config.json`。顶层结构如下：

- `listen_addr`
- `upstream_proxy`
- `web_rules`
- `custom_rules`
- `auto_detect`
- `management`
- `logging`

完整示例见 `configs/config.json.example`。

## 默认值

以下默认值由配置加载逻辑直接补全：

| 字段 | 默认值 |
| --- | --- |
| `listen_addr` | `localhost:1080` |
| `web_rules.enabled` | `true` |
| `web_rules.download_on_start` | `true` |
| `custom_rules.enabled` | `true` |
| `auto_detect.max_attempts` | `3` |
| `management.enabled` | `true` |
| `management.listen_port` | `9091` |
| `logging.level` | `info` |
| `logging.format` | `text` |
| `logging.max_size` | `10` |
| `logging.max_backups` | `5` |

未在表中的字段不会由当前加载逻辑补默认值，例如 `web_rules.url`、`web_rules.cache_path`、`custom_rules.path`、`auto_detect.rules_path`、`logging.file_path` 等。

## 路径字段说明

以下字段支持相对路径，并会在加载配置时相对于配置文件所在目录转换为绝对路径：

- `web_rules.cache_path`
- `custom_rules.path`
- `auto_detect.rules_path`
- `logging.file_path`

例如配置文件位于 `/opt/autoproxy/config.json`，当 `custom_rules.path` 写为 `custom.txt` 时，运行时接收到的实际路径会是 `/opt/autoproxy/custom.txt`。

如果调用 `config.Load` 时 `sourcePath` 为空字符串，则当前实现只会清理路径，不会把相对路径转换为绝对路径。

## 顶层字段

### `listen_addr`

- 类型：`string`
- 默认值：`localhost:1080`
- 作用：代理监听地址配置。
- 现状说明：当前运行时会保留该配置，但 `Runtime.Run(ctx)` 尚未真正启动代理监听。

### `upstream_proxy`

- 类型：`string`
- 默认值：无
- 作用：配置可选的上游代理地址，供代理处理器在需要时走上游转发。
- 可为空：是。

## `web_rules`

### `web_rules.enabled`

- 类型：`bool`
- 默认值：`true`
- 作用：控制是否启用在线规则功能。

### `web_rules.url`

- 类型：`string`
- 默认值：无
- 作用：在线规则下载地址。
- 说明：`ReloadWebRules` 在 URL 为空时会返回“未配置”错误。

### `web_rules.cache_path`

- 类型：`string`
- 默认值：无
- 作用：在线规则缓存文件位置。
- 说明：远端下载成功后，`WebSource.Load()` 会把原始内容写入该路径；远端失败时会尝试从该路径回退加载。

### `web_rules.refresh_interval`

- 类型：`int`
- 默认值：无
- 单位：秒
- 作用：描述在线规则自动刷新间隔。
- 现状说明：`WebSource` 已提供 `StartRefreshLoop` 能力，但当前 `Runtime.Run(ctx)` 尚未接入后台刷新循环，因此该字段目前主要作为配置结构保留与后续接入点。

### `web_rules.download_on_start`

- 类型：`bool`
- 默认值：`true`
- 作用：表示启动时是否立即下载在线规则。
- 现状说明：该字段已由配置解析与默认值逻辑支持，但当前 `Runtime.Run(ctx)` 尚未根据它自动触发下载。

## `custom_rules`

### `custom_rules.enabled`

- 类型：`bool`
- 默认值：`true`
- 作用：控制是否启用本地自定义规则。

### `custom_rules.path`

- 类型：`string`
- 默认值：无
- 作用：本地自定义规则文件路径。
- 说明：`ReloadCustomRules` 会读取该文件；文件不存在或解析失败时会返回错误。

## `auto_detect`

### `auto_detect.enabled`

- 类型：`bool`
- 默认值：无，零值为 `false`
- 作用：控制是否启用 auto-detect 回退与记录链路。

### `auto_detect.max_attempts`

- 类型：`int`
- 默认值：`3`
- 作用：代理层在直连失败后触发 auto-detect 回退前的最大尝试次数。

### `auto_detect.rules_path`

- 类型：`string`
- 默认值：无
- 作用：auto-detect 结果规则文件路径。
- 说明：新命中主机会追加写入该文件；本地规则重载时也会从这里重新加载 auto-detect 快照。若文件不存在，当前本地重载逻辑会把 auto-detect 视为空集合。

## `management`

### `management.enabled`

- 类型：`bool`
- 默认值：`true`
- 作用：表示是否启用管理接口特性。
- 现状说明：该开关会影响运行时特性摘要，但 `Runtime.Run(ctx)` 尚未自动启动管理 HTTP 监听。

### `management.listen_port`

- 类型：`int`
- 默认值：`9091`
- 作用：管理接口监听端口配置。
- 说明：CLI reload 命令会使用该端口构造 `http://127.0.0.1:<listen_port>` 作为本地管理地址。

## `logging`

### `logging.level`

- 类型：`string`
- 默认值：`info`
- 作用：日志级别。

### `logging.format`

- 类型：`string`
- 默认值：`text`
- 作用：日志格式。

### `logging.file_path`

- 类型：`string`
- 默认值：无，空字符串表示仅输出到标准输出
- 作用：日志文件路径。

### `logging.max_size`

- 类型：`int`
- 默认值：`10`
- 单位：MB
- 作用：单个日志文件的最大大小。

### `logging.max_backups`

- 类型：`int`
- 默认值：`5`
- 作用：日志文件最大保留数量。
