# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 3 实现、真机验收、签收与收尾）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1 ✅ 完成（tag `phase-1.end`）**
- **Phase 2 ✅ 完成（tag `phase-2.end`）**：8/8 验收，报告定稿，main CI 绿。
- **Phase 3 ✅ 完成（tag `phase-3.end`）**：A1~A8 通过；CI run 41066195833 全绿；报告 [docs/design/phase-3/report.md](../docs/design/phase-3/report.md)。
- 分支：`feat/phase3-permission-engine` 已合并 main；main 常绿；下一阶段 Phase 4 尚未立项。

## 上一轮完成了什么（3~5 条）

- 实现完整权限引擎：映射表唯一事实源、三内置预设 + 严格 `presets.d/*.yaml`、条件、DIM4 限额、持久规则（短语 `允许持久`、可查可删）、内容信任强制 once。
- 实现 JCS execHash/TOCTOU 管道（RFC 8785 + I-JSON、审批/工具记录落 hash、执行前重算与路径身份复核）与编译期内置 Hook（五点、优先级、超时、panic、warn/deny/fail_closed、改参白名单、secret-scan、配置指纹）。
- 桌面 API 与前端：预设选择与落库、规则面板、审批持久短语/信任/限额警示、`session:permission` 事件徽章。
- 真机验收（隔离 APPDATA + DeepSeek）：默认预设写文件三作用域审批、session 授权写成功、exec_hash 64 hex、content_trust 入库、Hook 指纹日志/顶栏一致；修复纯 tool_calls 未落 assistant 消息与 once 预设仍提供持久两个缺陷，另将 MCP unknown 纳入副作用。
- 文档收尾：Phase 3 报告定稿、CHANGELOG 0.1.011、看板/路线图勾选、P-009/P-010 沉淀、H-006 归档。

## 下一步（按优先级，注明阻塞项）

1. Phase 4 立项评审：不可变审计/hash-chain、正式回滚、遗留风险（路径残窗、配额持久化、工具决策徽章持久展示）；需所有者放行后开工。
2. 可选真机补录：持久短语与 secret-scan 的 GUI 提交（本轮受宿主安全分类器限制，由集成测试覆盖）。
3. 生成式绑定仍不可用；继续使用集中式 `Call.ByName`（`frontend/src/api.ts`）。

## 未决问题

- 工具决策徽章仅在运行期展示；transcript 重载后保留信任徽章，不保留 ask/session/限额徽章 → Phase 4 评估消息模型或回放。
- JCS 上游无 v1 tag；配额内存计数重启归零；路径二次校验与执行之间仍有残窗（沙箱前承认）。
- 外部进程 Hook/签名/TOFU：独立 ADR，不在 Phase 3。

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- 纯 tool_calls 回合必须落 assistant 消息（含 ToolCallsJSON），取消回合不得落悬空 tool_calls（P-009）。
- 预设收紧到 once 时必须关闭持久选项（P-010）；任何放宽通道都要看收紧后的作用域。
- 审批快照只在消费决策时比较一次（P-006）；终态先清 busy（P-005）。
- 崩溃恢复只标活动状态；运行中工具记 unknown（P-008）。
- Windows 控制台子进程必须 HideWindow；强杀才 taskkill /T /F（P-007）。
- 敏感文件硬拒不提供审批放行；密钥只走环境变量，不入库/日志/transcript。
- 渲染一律纯文本；Hook 改参仅 before_tool 白名单；after 只读；审计不落原文。
- 自动化测试/CI 禁外网；只有 manual 标签真机测试可调 DeepSeek；GUI 验收使用隔离 APPDATA。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
