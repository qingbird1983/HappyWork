# 03-ZCode UI 信息结构参照调研

- 状态：定稿（结构观察冻结；后续补充开新节）
- 日期：2026-10-30
- 关联：[ADR-0009](../adr/ADR-0009-zcode-ui-reference.md)；[architecture/04](../architecture/04-security-and-hooks.md) §渲染安全；UI 策略见 roadmap Backlog"UI/UX 精修"

## 背景与问题

所有者指定 ZCode 桌面客户端为 HappyWork 前端**信息结构**的参照物（偏好其操作习惯；灰盒阶段先对齐"呈现什么信息"，视觉后期再做）。本篇记录从参照物源码观察到的信息结构事实。

## 方法与来源

所有者本地 ZCode 源码（v3.16.2，pnpm monorepo，Electron + React，`packages/ui` 约 200+ 组件）；2026-10-30 读取组件清单与关键组件（PermissionDialog、ToolCallBlocks）的结构。**只读结构与命名，不复制任何代码**（该项目为 private 许可，standards/06 §6 / 07 §5 红线）。视觉级参照后续按 ADR-0009 流程向所有者索取截图。

## 事实与数据

### 主界面信息分区（组件名 → 信息结构）

| 分区 | 参照组件 | 信息结构要点 |
|---|---|---|
| 会话流 | `ModelTrajectoryExpandableMessage` + `ToolCallBlocks` | 消息可折叠/展开；工具调用以"块"呈现 |
| 工具调用块 | `ToolSummaryRow` / `ToolCallProgressIndicator` / `ToolCallBody` / `renderers/` | 折叠态一行摘要 + 进行中指示；展开看正文；**按工具类型分派专属渲染器**；另有 `ToolSnapshotFieldNotice`（字段快照提示） |
| 输入区 | `LexicalChatInput` + `chat-input-toolbar` | 富文本输入 + 工具栏（附件等） |
| 侧栏 | `GitPane`、任务列表、`WorkspaceSidebarFooter` | 工作区/任务导航 |
| 顶栏 | `ModelConfigSelect`、窗口控制 | 模型切换 + 桌面窗口操作 |
| 错误呈现 | `ChatErrorBanner`、`ModelTrajectoryErrorBlock` | 会话级 banner + 消息级错误块分层 |

### 审批弹窗（`PermissionDialog`）信息结构

- 选项四类：`allowOnce` / `allowAlways` / `deny` / `denyAlways`，每项 **label + description 成对**（标题 + 一句话解释后果）。
- 作用域规则（`PermissionRuleScope`）：展示"这条批准到底覆盖什么"，超长截断（约 160 字符）。
- `deny` 可携带自由文本反馈（升级为 refine feedback 回给模型）。
- MCP 工具带专门图标标识；来源徽章（`InteractionRequestOriginBadge`）标示请求来自谁。
- 另有 `ElicitationDialog`（结构化问询，模型主动向用户要信息）——区别于权限审批。

## 结论

1. 参照物与本项目模型**高度同构**：其 allowOnce/allowAlways+作用域 ≈ 本项目 once/session/persistent（DIM2）；其 deny+反馈 ≈ 状态机 `deny/feedback → thinking`；其工具块摘要/展开 ≈ transcript 工具结果展示需求。
2. 信息结构可直接借鉴：**审批选项"label+description 成对 + 作用域明示 + deny 带反馈"** 三要素值得照搬进灰盒。
3. 语义不照搬：参照物的 `allowAlways`（project 级）宽于本项目口径——本项目 session 绑 matchKey、persistent 需二次确认（ADR-0002/0003），按本项目更严的语义执行。

## 对本项目的影响

- Phase 1 施工稿的前端信息结构基线：会话流 + 工具块（摘要/展开/进度）+ 输入区；Phase 2 增加审批弹窗（三要素 + 三级 Info/Confirm/Block，架构 03）。
- 需要像素级/交互细节参照时：会话向所有者提出 → 所有者提供真实截图（投料 briefs）→ 结论沉淀回本篇"补充"节。
- 红线重申：参照物代码与视觉资产**不得复制**进本仓库；实现全部原创。

## 更新记录

- 2026-10-30：初稿（源码结构观察，冻结）。
