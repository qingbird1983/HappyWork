# 工作上下文交接卡

- 更新：2026-10-30（会话主题：v2 设计评审 → V3 基线落盘 + 仓库治理初始化）

## 当前阶段与状态（对照 roadmap 看板）

- Phase 0 ✅ 完成（2026-10-30）：仓库初始化、V3 设计基线、治理规范落盘、git 启用（tag `phase-0.end`）
- Phase 1（最小骨架）未开始；architecture/01~04 与 roadmap/00 处于**评审中**，定稿是 Phase 1 开工前置条件
- 看板：`docs/roadmap/00-roadmap.md` §1
- 版本控制状态：已启用 git（main 分支，首次提交含全部治理文件；briefs/ 按规则不入库）

## 上一轮完成了什么（3~5 条）

- v2 简报评审沉淀为 `docs/research/01-v2-design-review.md`（4 类发现 + 外部事实核实）
- V3 架构基线落盘：`docs/architecture/01~04`（权限合并、双指纹、路径规范化、备份式回滚、沙箱分期、上下文窗口管理、崩溃恢复为 v3 新增/修订点）
- 七项决策 ADR-0001~0007（技术栈 / 权限模型 / 双指纹+JCS / 路径规范化 / MVP 安全降级 / MIT / 前端 React+TS）
- 治理初始化：README×2、glossary、roadmap 看板 + phase-1 草稿、standards 回填（Go ≥1.26 + React/TS）、CI 门禁（go.mod 与 frontend/package.json 落地前跳过）、check-size 适配
- briefs 中 v2 简报已标记"已沉淀"（2026-10-30，去向 docs/）

## 下一步（按优先级，注明阻塞项）

1. 所有者评审 architecture/01~04 与 roadmap/00，转**定稿**；阻塞：需所有者过目（可在下次会话逐篇走）
2. 写 `docs/design/phase-1/` 施工设计稿（SQLite 驱动选型、mock Provider 行为定义、React 前端依赖准入与 eslint/prettier/tsc 配置）；阻塞：第 1 步定稿
3. Phase 1 开工：`git checkout -b feat/phase1-skeleton`

## 未决问题

- SQLite 驱动：modernc.org/sqlite（纯 Go、免 CGO）vs mattn/go-sqlite3（成熟、需 CGO）——Phase 1 设计稿决策
- matchKey 是否纳入"内容信任级"维度——Phase 3 评审（architecture/03 §待决）
- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- Wails v3 要求 Go ≥1.26，钉 minor 版本线；所有 Wails 调用必须走 `internal/desktop/` 适配层 → ADR-0001
- 权限判定只认"工具映射表"这一个事实源，角色预设只能收紧；matchKey（粗）与 execHash（细，JCS/RFC 8785）不得混用 → ADR-0002/0003
- 任何客体路径先过规范化管线再评估；Windows 大小写/8.3/junction 是已知逃逸向量 → ADR-0004、P-001
- 路线图工期是降级后的可信版本；不要把外部 Hook、Windows 沙箱、完整快照"顺手"提前 → ADR-0005
- 删模板/占位文件后记得全局 grep 引用是否悬空 → P-001
- 踩坑索引：P-001 初始化删模板文件后，残留引用会悬空（roadmap 曾引用 _phase-template.md）
