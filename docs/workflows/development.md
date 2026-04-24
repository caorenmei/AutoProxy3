# 开发流程

## 本地开发前提

- Go 1.24
- 一个可写的工作目录
- 建议在独立 Git worktree 中完成单项任务，避免与其他任务互相干扰

## 本地开发步骤

### 1. 获取依赖并确认代码可测试

```bash
go test ./...
```

该仓库目前以 Go 标准测试为主，优先使用 `go test ./...` 进行基线检查。

### 2. 准备配置文件

```bash
cp configs/config.json.example config.json
```

按需修改：

- `listen_addr`
- `web_rules.url`
- `custom_rules.path`
- `auto_detect.rules_path`
- `management.listen_port`
- `logging.file_path`

### 3. 运行命令行入口

```bash
go run ./src/cmd/autoproxy3 help
go run ./src/cmd/autoproxy3 version
go run ./src/cmd/autoproxy3 --config config.json serve
```

当前 `serve` 主要用于验证配置加载与运行时装配链路，不应把它当成已经完整启动代理/管理服务的长期运行入口。

### 4. 验证重载命令契约

如果本地存在与管理协议兼容的 HTTP 服务，可执行：

```bash
go run ./src/cmd/autoproxy3 --config config.json reload_web_rules
go run ./src/cmd/autoproxy3 --config config.json reload_custom_rules
go run ./src/cmd/autoproxy3 --config config.json reload_rules
```

若当前没有实际监听中的管理服务，这些命令会因连接失败而退出，这是符合当前实现边界的结果。

## 测试与覆盖率检查

### 全量测试

```bash
go test ./...
```

### 覆盖率快速检查

```bash
go test ./... -cover
```

### 覆盖率明细

```bash
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
```

`coverage.out` 会生成在仓库根目录；如无需保留，可在完成后手动删除。

## 文档同步要求

当变更涉及以下内容时，需要同步更新文档：

- 配置结构或默认值：更新 `README.md`、`configs/config.json.example`、`docs/guides/configuration.md`
- 管理接口路由或响应结构：更新 `README.md`、`docs/api/management.md`
- 运行时职责、reload 链路或 auto-detect 链路：更新 `docs/architecture/runtime.md`
- 开发命令或提交流程：更新 `docs/workflows/development.md`

文档内容必须与当前代码实现一致，不能提前描述尚未落地的启动行为或后台任务。

## 推荐提交流程

1. 在独立 worktree 中完成单一任务。
2. 先运行一次基线测试，确认当前分支可工作。
3. 修改代码或文档后再次执行相关测试。
4. 如变更涉及配置、接口或架构描述，同步更新对应文档。
5. 使用 `git status --short` 复核改动范围，只提交当前任务需要的文件。
6. 提交信息聚焦单一主题，并在提交说明末尾保留 `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`。
