# HappyWork

[English](README.md) · **简体中文**

**本地优先的受控桌面 AI Agent：最小权限、可逆优先、不可逆操作必经人工审批。**

更新日志：[CHANGELOG.md](CHANGELOG.md) · 许可证：[LICENSE](LICENSE)

## 亮点

- **受控而非全知**：权限判定收敛为单一工具映射表；"错了能不能撤销"先于"要不要放行"决定风险等级。
- **参数一致性管道**：审批看到什么就执行什么——规范化 + 执行锁定 hash，Hook 改参、路径逃逸、审批后漂移均被拦截。
- **本地优先**：会话记录存本地 SQLite，凭据只进系统钥匙串，无云端依赖，无静默自动更新。
- 文档先行的工程方式——设计文档、ADR、规范与阶段路线图先于实现代码落盘（见 [`docs/`](docs/README.md)）。

## 当前状态

Pre-code：V3 架构基线已落盘（评审中），Phase 1（最小骨架）未开始。

- **路线图与进度看板**：[docs/roadmap/00-roadmap.md](docs/roadmap/00-roadmap.md)

## 文档

项目文档以**中文**维护于 [`docs/`](docs/README.md)（本 README 双语入口之一）：

| 目录 | 内容 |
|---|---|
| [`docs/research/`](docs/research/) | 调研沉淀：事实与数据，决策的依据 |
| [`docs/adr/`](docs/adr/) | 决策记录（为什么这么做，编号递增，只增不改） |
| [`docs/architecture/`](docs/architecture/) | 架构设计：系统是什么样、关键约束 |
| [`docs/roadmap/`](docs/roadmap/) | 路线图：阶段范围、交付物、验收场景 |
| [`docs/standards/`](docs/standards/) | 仓库规范 9 篇：怎么做事的规则 |
| [`docs/design/`](docs/design/) | 各阶段详细设计稿与进度报告 |
| [`docs/glossary.md`](docs/glossary.md) | 术语表（面向非技术读者） |

## 待决策（需要项目所有者拍板的事项）

> 需要所有者决策的事项集中列在此表（两份 README 同步维护），并在 CHANGELOG 条目中提醒；决策后写 ADR 并从本表移除。

当前无待决策事项（前端框架已定 React，见 [ADR-0007](docs/adr/ADR-0007-frontend-react.md)）；新事项提出后登记于此。

## 许可证

MIT © 2026 The HappyWork Authors（详见 [LICENSE](LICENSE)；署名调整见 [ADR-0006](docs/adr/ADR-0006-license-mit.md)）
