# 工作上下文交接卡

- 更新：2026-10-30（会话主题：v2 设计评审 → V3 基线落盘 + 仓库治理初始化）

## 当前阶段与状态（对照 roadmap 看板）

- Phase 0 ✅ 完成（2026-10-30）：仓库初始化、V3 设计基线、治理规范落盘、git 启用（tag `phase-0.end`）
- Phase 1（最小骨架）未开始；architecture/01~04 与 roadmap/00 处于**评审中**，定稿是 Phase 1 开工前置条件
- 看板：`docs/roadmap/00-roadmap.md` §1
- 版本控制状态：已启用 git（main 分支，首次提交含全部治理文件；briefs/ 按规则不入库）

## 上一轮完成了什么（3~5 条）

- v2 简报评审沉淀为 research/01；V3 架构基线落盘 architecture/01~04；治理初始化完成（Phase 0，tag `phase-0.end`）
- 九项决策 ADR-0001~0009（技术栈 / 权限模型 / 双指纹+JCS / 路径规范化 / MVP 安全降级 / MIT / 前端 React+TS / 真机测试 DeepSeek / UI 参照 ZCode）
- 调研沉淀：research/02（DeepSeek API 对接事实）、research/03（ZCode UI 信息结构观察——审批弹窗"label+description+作用域+deny 反馈"三要素、工具块摘要/展开）
- **所有者授权**：开发期真机测试由 AI 会话执行（DeepSeek 真实 API，ADR-0008）；ZCode 截图按需向所有者索取（ADR-0009 流程）
- briefs：v2 简报已标已沉淀；B-001（ZCode 参照物备忘，含本地路径，不入库）

## 下一步（按优先级，注明阻塞项）

1. 所有者评审 architecture/01~04 与 roadmap/00，转**定稿**；阻塞：需所有者过目（可在下次会话逐篇走）
2. 写 `docs/design/phase-1/` 施工设计稿（SQLite 驱动选型、mock Provider 行为定义、React 前端依赖准入与 eslint/prettier/tsc 配置、前端信息结构基线引用 research/03）；阻塞：第 1 步定稿
3. Phase 1 开工：`git checkout -b feat/phase1-skeleton`

## 未决问题

- SQLite 驱动：modernc.org/sqlite（纯 Go、免 CGO）vs mattn/go-sqlite3（成熟、需 CGO）——Phase 1 设计稿决策
- matchKey 是否纳入"内容信任级"维度——Phase 3 评审（architecture/03 §待决）
- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3
- 本机 DeepSeek 环境变量名确认（代码默认读 `DEEPSEEK_API_KEY`，支持配置覆盖；所有者已配置 key）——Phase 2 接入时核对

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- Wails v3 要求 Go ≥1.26，钉 minor 版本线；所有 Wails 调用必须走 `internal/desktop/` 适配层 → ADR-0001
- 权限判定只认"工具映射表"这一个事实源，角色预设只能收紧；matchKey（粗）与 execHash（细，JCS/RFC 8785）不得混用 → ADR-0002/0003
- 任何客体路径先过规范化管线再评估；Windows 大小写/8.3/junction 是已知逃逸向量 → ADR-0004
- 路线图工期是降级后的可信版本；不要把外部 Hook、Windows 沙箱、完整快照"顺手"提前 → ADR-0005
- ZCode 是 private 项目：参照只读信息结构，**绝不复制代码/视觉资产**；需要截图走 ADR-0009 流程（向所有者明说界面+状态）
- DeepSeek key 只从环境变量读（默认 `DEEPSEEK_API_KEY`），严禁入库/入日志/入 transcript → ADR-0008、standards/06
- 前端一律灰盒推进（默认样式、零视觉投入），审批弹窗与来源标记的信息完整性不降级 → roadmap Backlog"UI/UX 精修"
- 删模板/占位文件后记得全局 grep 引用是否悬空 → P-001
- 踩坑索引：P-001 初始化删模板文件后，残留引用会悬空（roadmap 曾引用 _phase-template.md）
