# AutoProxy3 SPEC 收口重构设计

## 背景

当前 `feature/implement-spec` 已完成代理核心、规则引擎、日志模块，以及一部分管理接口客户端/服务端测试与实现，但距离 `SPEC.md` 的“全量交付”仍有明显缺口：

1. `serve` 路径尚未真正启动代理服务与管理服务。
2. 在线规则下载、启动时加载、本地缓存与定时刷新尚未接入运行时。
3. 管理接口尚未绑定真实的规则重载逻辑与运行时状态。
4. `README.md`、示例配置、项目文档、GitHub Actions 等交付物尚未补齐。

本次设计的目标不是继续在 `main` 上堆叠逻辑，而是先做一次收口式重构，把运行时生命周期、规则源管理和交付物整理成一个一致、可测试、可维护的整体。

## 设计目标

1. 以单进程模型完成 `SPEC.md` 中的全部运行时功能与交付物要求。
2. 通过新增统一启动编排层，把生命周期控制从 `cmd/autoproxy3/main` 中剥离出来。
3. 将规则源、管理接口、代理服务的依赖边界收敛为清晰接口，便于测试和后续维护。
4. 保持现有 `config`、`rules`、`proxy`、`logging` 模块的有效实现，避免无收益的大面积重写。
5. 满足 `AGENTS.md` 对简体中文文档、导出注释、工作流文档对齐和 100% 单元测试覆盖率的要求。

## 范围

本次重构与补齐范围包括：

1. 运行时功能：
   1. 代理服务启动与关闭。
   2. 管理服务启动与关闭。
   3. 在线规则下载、缓存、启动时加载与定时刷新。
   4. 本地规则与自动探测规则加载、重载与持久化。
   5. CLI `reload_*` 命令与管理接口联动。
2. 项目交付物：
   1. `README.md`
   2. `configs/config.json.example`
   3. 与实际实现对齐的 `docs/architecture/`、`docs/api/`、`docs/workflows/`、`docs/guides/`
   4. GitHub Actions CI 工作流

本次范围不包含：

1. 多进程拆分。
2. 热更新配置文件整体重载。
3. 非 `SPEC.md` 要求的扩展管理接口。

## 新增澄清结论

以下约束在设计审阅过程中追加确认：

1. 在线规则下载请求本身也要复用同一套规则判路语义。
2. 当 web 规则 URL 被当前规则集判定为应走代理，且配置了 `upstream_proxy` 时，下载请求必须经上游代理执行。
3. 首次启动且本地还没有 web 规则缓存时，由于系统无法提前知道 web 规则 URL 的判路结果，首次下载默认直连。
4. 一旦本地已有 web 规则缓存或当前进程已加载过 web 规则快照，后续下载与刷新都必须先根据当前规则集判定该 URL 是否应走上游。

## 总体方案

### 1. 新增统一启动编排层

新增 `src/internal/runtime` 作为唯一的运行时装配入口。它负责：

1. 接收已经解析完成的 `config.Config`。
2. 创建 logger、rules engine、proxy server、management server。
3. 执行启动阶段的规则加载。
4. 管理后台 web 规则刷新任务。
5. 暴露 `Run(context.Context) error` 或等价生命周期入口。

这样做之后，`cmd/autoproxy3/main` 只保留三个责任：

1. 解析命令行参数。
2. 加载配置文件。
3. 根据命令类型调用 runtime 或本地管理客户端，并转换退出码。

### 2. 新增规则源层

新增 `src/internal/rulesources`，把“规则从哪里来、如何加载、如何写回”从规则判定逻辑中分离出来。

建议拆成三个受控组件：

1. `WebSource`
   1. 负责 HTTP 下载。
   2. 负责 Base64 内容读取与解析委托。
   3. 负责更新缓存文件。
   4. 负责按 `refresh_interval` 定时刷新。
   5. 负责在下载前根据当前规则快照决定 web 规则 URL 的出站路由。
2. `FileSource`
   1. 负责读取本地 custom 规则文件。
   2. 负责读取 auto-detect 规则文件。
3. `AutoDetectStore`
   1. 负责向 auto-detect 规则文件追加主机。
   2. 负责去重与落盘格式一致性。

`src/internal/rules` 继续只负责：

1. 规则文本解析。
2. 内存快照替换。
3. 请求判路。

### 3. 管理接口保持薄层

`src/internal/management` 继续只做 HTTP handler，不直接知道配置文件路径或文件系统布局。它只接收 runtime 注入的回调：

1. 查询当前版本与功能状态。
2. 重载 web 规则。
3. 重载 custom + auto-detect 规则。
4. 重载全部规则。

管理接口的职责是把运行时动作投射为本地 HTTP API，而不是承载规则实现本身。

### 4. CLI 继续通过管理接口重载

`reload_web_rules`、`reload_custom_rules`、`reload_rules` 继续通过 `internal/cli` 访问本地管理接口，而不是在 CLI 进程里直接操作规则文件。

保留这个边界的原因是：

1. 可以确保命令作用于真实运行中的实例。
2. 管理接口和 CLI 共享同一套 reload 语义。
3. 集成测试更直接。

## 运行时数据流

### 启动流程

运行时启动按以下顺序执行：

1. 初始化日志。
2. 创建空规则引擎。
3. 加载 custom 规则文件。
4. 加载 auto-detect 规则文件。
5. 根据配置决定是否加载 web 规则：
   1. 若 `enabled=false`，跳过。
   2. 若 `download_on_start=true`，优先下载并刷新内存与缓存。
   3. 若本地已有 web 规则缓存，则先加载缓存，用它为后续下载建立初始判路依据。
   4. 若下载失败且本地缓存存在，则按设计回退到缓存快照继续启动。
   5. 若下载失败且缓存也不可用，则启动失败。
6. 创建 proxy server 与 management server。
7. 启动 web 规则后台刷新任务。
8. 监听并运行代理服务与管理服务。

### Web 规则刷新流程

后台刷新任务按固定节奏执行：

1. 下载远程规则。
2. 解析为新快照。
3. 成功时原子替换 engine 中的 web 快照。
4. 更新缓存文件。
5. 下载前先根据当前规则快照判定 web 规则 URL 是否应走上游；命中代理且已配置上游时，经上游执行下载。
6. 任一环节失败时仅记录日志并保留旧快照，不中断服务。

### Custom 与 Auto-detect 重载流程

`reload_custom_rules` 和 `reload_rules` 中的 custom 重载语义固定为：

1. 读取 custom 文件。
2. 读取 auto-detect 文件。
3. 仅当两者都解析成功时，原子替换两个快照。
4. 任一失败时，保持原有内存快照不变，并向管理接口响应返回明确错误。

### Auto-detect 持久化流程

自动探测命中后：

1. 由 proxy 调用 runtime 注入的记录接口。
2. 记录器将主机追加到 auto-detect 文件。
3. 追加成功后刷新 engine 中的 auto-detect 快照。
4. 追加失败只记录日志，不回滚已经成功的请求转发。

## 错误处理策略

### 启动阶段

采用 fail-fast：

1. 配置文件无法读取或解析，直接退出。
2. 日志初始化失败，直接退出。
3. custom / auto-detect 初始加载失败，直接退出。
4. 启动阶段需要生效的 web 规则无法建立有效初始状态，直接退出。
5. 代理或管理监听失败，直接退出。

### 运行阶段

运行期不因为单次规则刷新失败而中断：

1. web 定时刷新失败：记录错误，保持旧快照。
2. 管理接口单次 reload 失败：返回失败响应，保持旧快照。
3. auto-detect 写盘失败：记录错误，不影响已完成请求。

## 模块职责边界

### `src/cmd/autoproxy3`

1. 参数解析。
2. 配置加载。
3. 调用 runtime 启动。
4. 调用本地管理客户端执行 `reload_*`。

### `src/internal/runtime`

1. 统一组装依赖。
2. 维护 server lifecycle。
3. 管理后台任务。
4. 作为管理接口与 proxy 的协作中介。

### `src/internal/rulesources`

1. 文件加载。
2. 远程下载。
3. 缓存更新。
4. auto-detect 落盘。

### `src/internal/management`

1. 只负责 HTTP 路由与 JSON 响应格式。
2. 不负责文件路径、规则解析和引擎细节。

### `src/internal/proxy`

1. 只负责请求转发与自动探测触发。
2. 不负责后台刷新、规则文件读取和管理接口协议。

## 测试设计

本次重构按 TDD 推进，测试分为三层：

### 1. Runtime 单元测试

覆盖：

1. 启动时依赖装配顺序。
2. 初始规则加载成功/失败分支。
3. 后台刷新任务的启停。
4. 管理接口回调是否调用真实 runtime reload 逻辑。

### 2. Rulesources 单元测试

覆盖：

1. web 下载成功、下载失败、缓存回退。
2. refresh interval 为 0 时不启动后台刷新。
3. custom / auto-detect 文件加载与错误路径。
4. auto-detect 追加与去重。

### 3. 集成测试

覆盖：

1. 启动完整 runtime 后代理服务可处理请求。
2. 管理接口可返回状态与执行 reload。
3. CLI `reload_*` 能命中本地管理接口。
4. web 刷新后规则判定生效。
5. auto-detect 写盘后后续请求可命中新规则。

最终要求：

1. `go test ./...` 通过。
2. `go test -cover ./...` 中所有有测试的包保持 100% 覆盖。

## 文档与交付物同步

代码完成后需同步补齐并对齐以下文件：

1. `configs/config.json.example`
2. `README.md`
3. `docs/architecture/runtime.md`：描述运行时装配与生命周期。
4. `docs/api/management.md`：描述管理接口输入输出。
5. `docs/workflows/development.md`：描述开发、测试、CI 流程。
6. `docs/guides/configuration.md`：描述配置项和运行方式。
7. `.github/workflows/ci.yml`：执行测试与覆盖率检查。

文档必须使用简体中文，并且以最终代码行为为准。

## 推荐实现顺序

1. 重构 `cmd/autoproxy3`，引入 `internal/runtime` 但先只保留最小启动骨架。
2. 实现 `internal/rulesources` 的文件加载、下载、缓存与追加能力。
3. 把 runtime 与 `rules`、`proxy`、`management` 接通。
4. 为 runtime 与 rulesources 补全测试并让全量测试恢复为绿色。
5. 补齐 README、示例配置、架构/API/工作流文档。
6. 增加 GitHub Actions 工作流。

## 取舍说明

之所以采用“先大重构再实现”的方案，而不是继续在现有 `main` 和 `management` 上增量堆叠，是因为当前缺口已经集中在**生命周期编排**与**规则源协作**上。如果不先把这两个中心收口：

1. `main` 会继续膨胀成不可测试的装配脚本。
2. 管理接口会被迫知道过多底层细节。
3. web 刷新、auto-detect、reload 三条路径会出现重复逻辑。
4. 文档和 CI 难以围绕一个稳定架构对齐。

本设计的重构幅度虽然高于增量补丁，但仍刻意限制在单一二进制、现有模块延续、目录级新增两个包以内，确保它仍然是一次可在当前分支内完成的收口重构，而不是另起炉灶。
