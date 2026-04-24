# 管理接口

## 概览

管理接口处理器定义在 `src/internal/management/server.go`。当前代码已经实现完整的路由、方法校验与 JSON 响应格式；CLI 中的 `reload_web_rules`、`reload_custom_rules`、`reload_rules` 默认都会访问本地管理地址 `http://127.0.0.1:<management.listen_port>`。

需要注意：当前 `Runtime.Run(ctx)` 尚未自动启动管理 HTTP 监听，因此本文描述的是已经实现的接口契约，而不是 `serve` 命令当前已经对外托管的完整进程行为。

## 通用规则

- 首页只接受 `GET`。
- 重载接口只接受 `POST`。
- 方法不匹配时返回 JSON 错误，HTTP 状态码为 `405 Method Not Allowed`。
- 重载失败时返回 JSON 错误，HTTP 状态码为 `500 Internal Server Error`。
- 响应头 `Content-Type` 为 `application/json`。

### 方法错误示例

```http
GET /reload_web_rules HTTP/1.1
```

```json
{
  "success": false,
  "error": "method not allowed"
}
```

## `GET /`

返回管理首页摘要，包含版本号、启用特性与规则加载状态。

### 成功响应示例

```json
{
  "success": true,
  "version": "0.1.0",
  "features": [
    "proxy",
    "web-rules",
    "custom-rules",
    "management"
  ],
  "rules": {
    "web": {
      "enabled": true,
      "loaded": true
    },
    "custom": {
      "enabled": true,
      "loaded": false
    },
    "auto_detect": {
      "enabled": false,
      "loaded": false
    }
  }
}
```

### 字段说明

- `success`：固定为 `true`。
- `version`：来自 `buildinfo.Version`。
- `features`：由运行时根据配置启用项生成。
- `rules`：当前 web、custom、auto-detect 三类规则的启用与已加载状态。

## `POST /reload_web_rules`

重载在线规则。底层会调用运行时的 `ReloadWebRules(ctx)`；只有在线规则加载成功后才会替换引擎中的 web 快照。

### 成功响应示例

```json
{
  "success": true,
  "partial": false,
  "steps": [
    {
      "name": "reload_web_rules",
      "success": true
    }
  ]
}
```

### 失败响应示例

```json
{
  "success": false,
  "partial": false,
  "error": "web rules source is not configured",
  "steps": [
    {
      "name": "reload_web_rules",
      "success": false,
      "error": "web rules source is not configured"
    }
  ]
}
```

## `POST /reload_custom_rules`

统一重载本地规则。虽然路径名叫 `reload_custom_rules`，但当前语义是一起处理：

- `custom_rules`
- `auto_detect.rules_path`

也就是说，该接口会调用运行时的统一本地规则加载逻辑，同时替换 custom 与 auto-detect 两类快照。

### 成功响应示例

```json
{
  "success": true,
  "partial": false,
  "steps": [
    {
      "name": "reload_custom_rules",
      "success": true
    }
  ]
}
```

### 失败响应示例

```json
{
  "success": false,
  "partial": false,
  "error": "open custom rules: open /path/to/custom-rules.txt: no such file or directory",
  "steps": [
    {
      "name": "reload_custom_rules",
      "success": false,
      "error": "open custom rules: open /path/to/custom-rules.txt: no such file or directory"
    }
  ]
}
```

## `POST /reload_rules`

依次重载在线规则与本地规则，执行顺序为：

1. `reload_web_rules`
2. `reload_custom_rules`

### 全部成功响应示例

```json
{
  "success": true,
  "partial": false,
  "steps": [
    {
      "name": "reload_web_rules",
      "success": true
    },
    {
      "name": "reload_custom_rules",
      "success": true
    }
  ]
}
```

### 部分失败响应示例

当在线规则成功而本地规则失败时，当前实现仍返回 `500`，并用 `partial: true` 表示部分成功：

```json
{
  "success": false,
  "partial": true,
  "error": "custom reload failed",
  "steps": [
    {
      "name": "reload_web_rules",
      "success": true
    },
    {
      "name": "reload_custom_rules",
      "success": false,
      "error": "custom reload failed"
    }
  ]
}
```

## 与 CLI 的关系

CLI 默认命令映射如下：

- `autoproxy3 reload_web_rules` → `POST /reload_web_rules`
- `autoproxy3 reload_custom_rules` → `POST /reload_custom_rules`
- `autoproxy3 reload_rules` → `POST /reload_rules`

CLI 在收到非 2xx 或 `success=false` 的 JSON 响应时，会优先把 `error` 字段或步骤里的首个错误转换为命令行错误输出。
