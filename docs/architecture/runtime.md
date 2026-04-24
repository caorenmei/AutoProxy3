# 运行时架构

## 概览

当前版本的 `Runtime` 位于 `src/internal/runtime/runtime.go`，职责是把配置、规则引擎、规则源、管理接口处理器与代理处理器装配到同一个最小运行时对象中。它已经实现规则重载、状态摘要与 auto-detect 记录回写链路，但 `Run(ctx)` 仍然只保留最小入口：仅检查 `ctx.Err()`，不会在当前版本中启动长期运行的网络服务。

## 运行时持有的核心依赖

`Runtime` 直接持有以下依赖，而不是通过额外间接层包装：

- `rules.Engine`：统一保存 web、custom、auto-detect 三类规则快照，并提供判路能力。
- `rulesources.WebSource`：负责下载在线规则、必要时回退缓存，并提供后台刷新循环入口。
- `rulesources.FileSource`：负责一次性读取本地 `custom_rules` 与 `auto_detect` 规则文件。
- `rulesources.AutoDetectStore`：负责将自动探测命中的主机名追加写入规则文件。
- `management.Server`：暴露状态摘要与规则重载处理器。
- `proxy.Server`：承接代理判路、上游转发与 auto-detect 结果记录。

## 装配边界

`runtime.New(cfg)` 负责完成以下装配：

1. 创建空的 `rules.Engine`。
2. 根据 `cfg.WebRules.Enabled` 与 URL/缓存路径构造 `WebSource`。
3. 使用 `cfg.AutoDetect.RulesPath` 构造 `AutoDetectStore`。
4. 将 `StatusSummary`、`ReloadWebRules`、`ReloadCustomRules` 注入 `management.Server`。
5. 将 `rules.Engine`、上游代理设置、auto-detect 开关、最大尝试次数与 recorder 注入 `proxy.Server`。

这里的管理服务与代理服务在当前阶段只是被装配完成，并未由 `Run(ctx)` 启动监听。

## 规则重载链路

### 在线规则重载

`ReloadWebRules(ctx)` 的行为如下：

1. 检查上下文是否已取消。
2. 当 `web_rules.enabled` 为 `false`，或者 URL 为空时，直接返回配置错误。
3. 调用 `webSource.Load()` 下载并解析规则；下载失败时，`WebSource` 会尝试读取缓存文件。
4. 只有在 `Load()` 成功返回后，才调用 `engine.ReplaceWebRules(set)` 替换 web 规则快照。
5. 将运行时中的 web 已加载状态更新为 `true`。

因此，在线规则重载是“成功后替换”的语义，失败不会破坏旧快照。

### 本地规则重载

`ReloadCustomRules(ctx)` 的行为如下：

1. 检查上下文是否已取消。
2. 调用 `fileSource.LoadCustomAndAutoDetect(customPath, autoDetectPath)` 一次性读取两类本地规则。
3. 当 custom 文件打开或解析失败时，整体返回错误。
4. 当 auto-detect 文件不存在时，当前实现会把 auto-detect 视为空集合而不是报错。
5. 只有在读取成功后，才同时执行 `engine.ReplaceCustomRules(...)` 与 `engine.ReplaceAutoDetectRules(...)`。
6. 成功后同步刷新 `custom`、`auto-detect` 的 loaded 状态。

`POST /reload_custom_rules` 与 CLI `reload_custom_rules` 使用的就是这条统一链路，因此它不仅刷新 `custom_rules`，也会同步刷新本地 auto-detect 快照。

## auto-detect 协作链路

当前 auto-detect 记录链路已经闭环，但触发点在代理处理器内部：

1. `proxy.Server` 根据规则引擎结果决定直连、走上游，或在失败达到阈值后走 auto-detect 回退。
2. 当某个目标主机通过 auto-detect 回退成功建立连接后，代理层调用运行时注入的 recorder。
3. recorder 先调用 `AutoDetectStore.AppendHost(host)` 将主机名写入 `auto_detect.rules_path`。
4. 写盘成功后，运行时立即调用 `engine.AddAutoDetectHost(host)` 更新内存中的 auto-detect 快照。
5. 若新增主机实际进入引擎快照，则运行时把 `auto-detect` 的 loaded 状态更新为启用态。

这意味着 auto-detect 不需要等待下一次显式重载，内存快照会在写盘成功后即时同步。

## 状态摘要

`StatusSummary()` 会返回三类规则的摘要：

- `web.enabled` / `web.loaded`
- `custom.enabled` / `custom.loaded`
- `auto_detect.enabled` / `auto_detect.loaded`

该摘要被管理接口首页 `GET /` 直接使用，用于对外暴露当前运行时的规则加载状态。

## 当前未接入的部分

虽然配置中已经存在 `web_rules.refresh_interval` 与 `web_rules.download_on_start`，且 `WebSource` 也提供了 `StartRefreshLoop`，但当前 `Runtime.Run(ctx)` 还没有把这些配置接入实际启动流程。编写部署或使用文档时，应明确把这些字段描述为“当前配置结构和规则源能力的一部分”，不能描述成“当前版本已经自动启动后台刷新任务”。
