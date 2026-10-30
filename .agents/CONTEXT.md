# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 4 立项草案；Phase 3 已收尾）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1 ✅ 完成（tag `phase-1.end`）**
- **Phase 2 ✅ 完成（tag `phase-2.end`）**：8/8 验收，报告定稿，main CI 绿。
- **Phase 3 ✅ 完成（tag `phase-3.end`）**：A1~A8 通过；CI run 41066195833 全绿；报告 [docs/design/phase-3/report.md](../docs/design/phase-3/report.md)。
- **Phase 4 进行中（2026-10-30 立项放行）**：范围合同、施工设计、发布签名设计、ADR-0015/0016/0017 已定稿；D1~D6 已放行。
- 分支：`feat/phase4-audit-rollback-signing` 开工（main 常绿）。

## 上一轮完成了什么（3~5 条）

- Phase 3 完整收尾：权限引擎、Hook、execHash/TOCTOU、真机验收、报告、CI、合并 main 与 tag `phase-3.end`。
- 起草并放行 Phase 4：范围合同、施工设计、发布签名设计、ADR-0015/0016/0017；D1~D6 全部定稿。
- 登记 docs/README、看板、CHANGELOG 0.1.012/0.1.013、术语表与 standards/06 发布签名凭据纪律。
- 分支 `feat/phase4-audit-rollback-signing` 开工；未改实现代码。

## 下一步（按优先级，注明阻塞项）

1. 实现迁移 0004：审计链字段/触发器/`audit_chain`、`file_backups`。
2. 实现 `internal/audit`（哈希/验证/导出）与 `internal/rollback`（版本/恢复/撤销/写后校验/配额）。
3. agent/tool/desktop 接线：审计覆盖、trace、备份 API、fail-closed。
4. 前端审计/备份面板；`.github/workflows/release.yml` dry-run。
5. 全量门禁、A1~A8 验收报告；真实签名 A7 视凭据补跑。
6. 可选真机补录（Phase 3 遗留）：持久短语与 secret-scan 的 GUI 提交（宿主安全分类器限制，集成测试已覆盖）。

## 未决问题

- 真实签名凭据是否存在；没有凭据时 A7 记条件未执行，不伪造证据。
- 审计链威胁模型需所有者知悉：本地可检测局部篡改，不防全权限攻击者重算整链；无外部锚定。
- 备份保留 20 版/512 MiB 软上限是否合适；恢复/撤销仅在会话非运行态。
- 工具决策徽章仍只在运行期展示；transcript 重载后不保留 ask/session/限额徽章（Phase 4 不处理）。

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- 文档先行：Phase 4 文档未定稿/未放行前不写实现代码。
- 审计放行类事件 fail-closed：写不进审计就不放行；拒绝类只告警。不得伪造"历史已密封"。
- 备份恢复前先备份当前状态；新建撤销移入 `discarded/`，不直接删除；路径每次重规范化。
- 无真实凭据不得发布未签名产物；dry-run 产物只能是 preview。
- 纯 tool_calls 回合必须落 assistant 消息（P-009）；预设收紧到 once 时关闭持久（P-010）；审批快照只比较一次（P-006）；崩溃恢复只标活动状态（P-008）。
- 敏感文件硬拒不提供审批放行；密钥只走环境变量，不入库/日志/transcript。
- 渲染一律纯文本；Hook 改参仅 before_tool 白名单；after 只读；审计不落原文。
- 自动化测试/CI 禁外网；只有 manual 标签真机测试可调 DeepSeek；GUI 验收使用隔离 APPDATA。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
