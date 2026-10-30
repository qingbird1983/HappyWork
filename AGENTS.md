# AGENTS.md —— AI 协作入口（薄索引）

> 本文件**只做导航与铁律**，禁止记录状态、进度、阶段指针或任何流水账。
> AI 的"当前状态"唯一来源：`.agents/CONTEXT.md`（第一站，先读它）。
> 进度看板唯一在 `docs/roadmap/00-roadmap.md`；进展流水唯一在 `CHANGELOG.md`。

## 文档索引（按需进入）

| 你要做什么 | 去哪里 |
|---|---|
| 了解项目 / 对外介绍 | [README.md](README.md)（英）/ [README.zh-CN.md](README.zh-CN.md)（中，两份内容一致） |
| 接手工作 / 看当前状态 | [.agents/CONTEXT.md](.agents/CONTEXT.md) → 交接规则见 [.agents/README.md](.agents/README.md) |
| 看进度 / 阶段规划 | [docs/roadmap/00-roadmap.md](docs/roadmap/00-roadmap.md)（唯一看板）+ 当前 phase 文档 |
| 查"为什么这么设计" | [docs/adr/](docs/adr/)（编号递增，只增不改） |
| 查系统设计与约束 | [docs/architecture/](docs/architecture/)（先读 01 总览） |
| 查做事规则 | [docs/standards/](docs/standards/)（9 篇；规范自身的复查与进化见 [standards/09](docs/standards/09-standards-review.md)） |
| 写码纪律（写代码时的默认工作方式） | [.agents/skills/ponytail/](.agents/skills/ponytail/SKILL.md)——最小完整改动（MIT，vendored，语言无关） |
| 查术语 | [docs/glossary.md](docs/glossary.md) |
| 读所有者的参考/建议材料 | [briefs/](briefs/README.md)——本地输入区（不入库）；对齐后结论必须沉淀进 docs/ 对应区域 |
| 新增文档登记 | [docs/README.md](docs/README.md)（文档地图，禁止孤儿文档） |

## 铁律（细则见链接，违反即返工）

1. **文档先行**：当前阶段设计文档未定稿，禁止写实现代码 —— [standards/03](docs/standards/03-documentation.md)。
2. **决策留痕**：重大技术决策必须新增 ADR，不得只做不记 —— [standards/03](docs/standards/03-documentation.md)。
3. **收尾四件事**：重写 CONTEXT 交接卡、归档 handoff、沉淀 pitfalls/skills、体量核对；教训当轮提取 —— [.agents/README.md](.agents/README.md)、[standards/07 §7](docs/standards/07-ai-collaboration.md)。
4. **安全红线**：密钥、个人路径不入仓库；外部代码按许可证纪律使用 —— [standards/06](docs/standards/06-security.md)。
5. **范围纪律**：只做当前阶段"做"清单内的事，超范围先记 backlog 或写 ADR，不顺手实现 —— [standards/07](docs/standards/07-ai-collaboration.md)。
6. **体量红线**：docs / .agents / 代码 / 依赖的预算见 [standards/08](docs/standards/08-size-control.md)，超标当期处置或登记豁免。
7. **修 bug 先写复现测试**：能复现的失败测试先于修复代码，且永久保留 —— [standards/05](docs/standards/05-testing.md)。

> 语言约定：文档与对用户的沟通用中文；代码、标识符、commit message 用英文。
