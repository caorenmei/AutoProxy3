# AGENTS.md - 核心指令与工作规范

此文件定义了在本项目中与 Code Agent 协作的最高准则。Code Agent 在执行任何任务前必须阅读并严格遵守。

## 🛠 工作流 (Superpowers Workflow)

### 严格遵循

必须使用 **Superpowers** 工作流，确保每一步都符合预定的流程和质量标准。
  
### 文档对齐

在执行工作流的各阶段（Plan/Execute/Review），必须实时对齐 `docs/` 目录下的相关文档。任何逻辑变更、架构调整或新增功能，必须同步更新工作流文档以及项目整体文档，确保代码实现与文档描述保持 100% 一致。
  
### **Git Worktree**、**Pull Request** 与 **Github Actions**

使用 **Git Worktree** 进行开发。
通过 **Pull Request** 提交代码。
监视 **Pull Request** 的状态，当 **Github Actions** 的 **CI/CD** 流程显示所有测试用例通过后，合并代码到主分支。

## 🇨🇳 语言规范
- **工作语言**：所有对话交互、任务规划与总结、思考过程等必须使用**简体中文**。
- **文档与注释**：所有生成的文档（Markdown）、代码注释（JSDoc/Docstring）等必须使用**简体中文**。
- **代码与字符串**：如非特别指定，新增或修改的代码、测试代码、配置键名、命令示例、字符串字面量（包括测试断言消息与新增运行时字符串）默认只使用英文；未触及的存量代码按现状保留。文档与注释中的技术专用缩写、协议名及约定俗成术语可保留原文。

## 📝 注释与文档
- **公共成员**：所有公共接口 (Public APIs)、导出类 (Exported Classes) 和公共方法 (Public Methods) **必须** 包含详尽的简体中文注释。
- **内容要求**：注释需涵盖功能描述、输入参数说明、返回值定义及可能抛出的异常。

## ❓ 交互准则
- **强制询问**：**总是使用 `Ask User` 工具** 向我提问。
- **继续执行**：完成一轮任务后，**必须使用 `Ask User` 工具** 提出与当前上下文相关的后续问题，引导项目持续迭代。

## 📂 项目结构
项目应遵循以下目录布局以确保代码与文档的同步性：

```text
.
├── .agents/
│   └── skills/
├── AGENTS.md
├── docs/
│   ├── ddd/               # 领域驱动设计 (DDD) 相关文档
│   │   ├── 01-glossary/   # 术语与概念
│   │   │   ├── ubiquitous-language.md
│   │   │   └── adr-20260101-01-naming.md 
│   │   ├── 02-strategic/  # 战略设计
│   │   │   ├── context-map.md
│   │   │   ├── domains-definitions.md
│   │   │   └── adr-20260102-01-bc-boundary.md 
│   │   ├── 03-tactical/   # 战术设计
│   │   │   └──── ordering-context/
│   │   │       ├── models.md
│   │   │       ├── domain-events.md  
│   │   │       ├── services.md       
│   │   │       └── adr-20260103-01-aggregate.md
│   │   ├── 04-scenarios/  # 业务场景与用户旅程
│   │   │   ├── event-storming.md
│   │   │   ├── user-journeys/
│   │   │   └── adr-20260104-01-consistency.md
│   │   ├── 05-adr/        # 架构决策记录 (ADR)
│   │   │   └── 20260105-01-record-architecture-decisions.md 
│   │   └── 06-standard/   # 设计规范与最佳实践
│   │       ├── api-spec.md           
│   │       ├── coding-guidelines.md  
│   │       └── adr-20260106-01-dto-policy.md
│   ├── architecture/      # 架构决策与核心逻辑
│   ├── api/               # 接口定义与协议说明
│   ├── workflows/         # 开发、部署与业务流程
│   ├── guides/            # 开发规范与专项指南
│   └── superpowers/       # Superpowers 工作流文档
├── src/                   # 源代码目录
└── README.md              # 项目总体说明 (简体中文)

```
