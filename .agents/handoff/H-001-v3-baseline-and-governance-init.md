# H-001-V3 基线落盘与治理初始化

- 日期：2026-10-30
- 范围：Pre-code（briefs 评审 → docs 基线 → 仓库治理初始化 → git 启用）

## 本轮做了什么

1. 评审所有者简报《Go+Wails 3 桌面端 Agent 设计方案 v2》，发现沉淀为 `docs/research/01-v2-design-review.md`（4 类问题 + 外部事实核实）。
2. V3 架构基线落盘 `docs/architecture/01~04`；六项决策写 ADR-0001~0006（Go+Wails3、单一权限模型、双指纹+JCS、路径规范化、MVP 安全降级、MIT）。
3. 治理初始化：README×2、glossary（20 词条）、roadmap 看板（Phase 0 完成，Phase 1~5 规划）+ phase-1 草稿、standards/01/04/05/06/07/08 回填、CI（go.mod 守卫）、check-size 适配、LICENSE(MIT)、模板与占位目录清理。
4. 所有者三项决策（AskUserQuestion）：项目名 HappyWork；许可证 MIT；立即启用 git。

## 验证

- `scripts/check-size`：全部通过（见本轮收尾输出）。
- 全局占位符扫描（全角方括号）：除 briefs/（所有者本地输入区）与 phase 文档"一票否决"约定标记外无残留。
- briefs v2 简报头部已标"已沉淀（2026-10-30，去向 docs/）"。

## 遗留与下一步

- architecture/01~04、roadmap/00 为**评审中**——需所有者过目后转定稿（Phase 1 开工前置）。
- 下一步：design/phase-1/ 施工设计稿（前端已定 React+TS，ADR-0007）→ 定稿 → feat/phase1-skeleton 开工。

## 已提取的沉淀

- P-001（初始化删模板后引用悬空）
