# 文档地图

本目录是项目的**唯一事实来源**。任何会话、任何人都应能只靠这些文档理解项目并继续工作。

每份文档头部有状态标记：**草稿 → 评审中 → 定稿**。阶段开工的前提是相关设计文档已达「定稿」（流程见 [standards/03](standards/03-documentation.md)）。

## 阅读顺序建议

| 你是谁 | 读什么 |
|---|---|
| 项目所有者（不读代码） | [README.zh-CN.md](../README.zh-CN.md) → [roadmap/00-roadmap.md](roadmap/00-roadmap.md)（看板）→ CHANGELOG → `adr/`（感兴趣时） |
| 新加入的 AI 会话 | `.agents/CONTEXT.md` → [AGENTS.md](../AGENTS.md) → 本页 → 当前 phase 文档 → `standards/` 全部 → 相关架构文档 |
| 未来的人类协作者 | 同 AI 会话路径 |

## 目录结构

```
.agents/                AI 知识库与交接中枢（仓库根；规范见其 README 与 standards/08）
docs/
├── README.md            本页：文档地图
├── glossary.md          术语表（面向非技术读者）
├── research/            调研沉淀（事实与数据，决策的依据）
├── adr/                 决策记录（为什么这么做，编号递增，只增不改）
├── architecture/        架构设计（系统是什么样、关键约束）
├── roadmap/             路线图（什么时候做什么、怎么验收）
├── standards/           仓库规范（怎么做事的规则，9 篇）
└── design/              各阶段详细设计稿与阶段进度报告（design/phase-N/，随阶段建立）
仓库根另有：briefs/（所有者本地输入区，不入库）、scripts/ 脚本
```

代码目录布局（仓库根）见 [standards/01](standards/01-repo-structure.md) §2。

## 文档清单与状态

> 规则：新增文档必须同步登记本表；改状态必须同步本表与 CHANGELOG。

| 文档 | 状态 | 一句话说明 |
|---|---|---|
| roadmap/00-roadmap.md | 评审中 | 总路线图、唯一进度看板、backlog 与风险登记簿 |
| roadmap/phase-1-minimal-skeleton.md | 草稿 | Phase 1 范围合同：最小骨架的做/不做与验收场景 |
| standards/01 ~ 09 | 定稿 | 仓库结构 / git 工作流 / 文档 / 代码 / 测试 / 安全 / AI 协作 / 体量 / 规范复查（01/04/05 已按本项目回填） |
| glossary.md | 草稿 | 术语表（面向非技术读者） |
| research/01-v2-design-review.md | 定稿 | v2 设计简报评审：4 类发现与外部事实核实，V3 修订依据 |
| adr/ADR-0001-adopt-go-wails3.md | 定稿 | 技术栈选型 Go + Wails v3，钉版本 + 适配层隔离 |
| adr/ADR-0002-unified-permission-model.md | 定稿 | 权限模型合并为单一工具映射表，角色降级为预设包 |
| adr/ADR-0003-dual-fingerprint.md | 定稿 | 审批匹配指纹与执行锁定 hash 分离；canonical JSON 采用 JCS |
| adr/ADR-0004-path-canonicalization.md | 定稿 | 客体路径强制规范化前置，堵住前缀检查逃逸 |
| adr/ADR-0005-mvp-security-downgrade.md | 定稿 | MVP 安全降级：备份式回滚、沙箱分期、外部 Hook 延后 |
| adr/ADR-0006-license-mit.md | 定稿 | 项目许可证采用 MIT |
| architecture/01-overview.md | 评审中 | 定位与边界、六层架构、包依赖、代码骨架、阶段映射 |
| architecture/02-core-loop-and-data.md | 评审中 | 会话 Actor、完整状态机、预算、上下文窗口管理、数据模型、崩溃恢复 |
| architecture/03-permission-and-approval.md | 评审中 | 单一权限模型、四维审批、双指纹、路径规范化管线、工具映射表 |
| architecture/04-security-and-hooks.md | 评审中 | 三层防线、TOCTOU 管道、Hook、渲染安全、沙箱分期、备份回滚、测试基线 |
