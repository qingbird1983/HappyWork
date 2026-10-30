# ADR-0015：审计日志默认哈希链密封，追加只读，无外部锚定

- 状态：定稿（2026-10-30，随 Phase 4 立项放行）
- 日期：2026-10-30
- 关联：[architecture/04](../architecture/04-security-and-hooks.md) §审计与操作后校验；[phase-4-audit-rollback-signing.md](../roadmap/phase-4-audit-rollback-signing.md)；[design.md](../design/phase-4/design.md) §4；[ADR-0005](ADR-0005-mvp-security-downgrade.md)

## 背景

Phase 1–3 的 `audit_log` 只有 `session_id/action/decision/reason/created_at`，无链式哈希，且实际只在拒绝、规则命中与 Hook 事件写入。architecture/04 原写"hash-chain 密封为可选开关（合规需求时开启）"，但 Phase 4 的主题就是"不可变审计"：可关闭的审计无法支撑"出错可追溯"，也无法在升级后诚实回答"这条记录是否被改过"。

本地桌面应用没有可信远端：同一用户权限下的攻击者可以改数据库、删触发器，甚至重写整条链。因此需要先定义威胁模型，避免把本地完整性护栏宣传成不可篡改证明。

## 决策

1. **审计链默认开启，不提供用户关闭开关**。所有安全事件（工具判定、审批、持久规则、Hook、备份/恢复、链验证、启动恢复）写入 `audit_log`。
2. `audit_log` 增加 `turn_id/trace_id/detail_json/prev_hash/entry_hash`；`entry_hash = SHA-256(JCS(v=1, prev, 规范字段))`，复用 `internal/jcs`（[ADR-0012](ADR-0012-jcs-implementation.md)）。`id` 不参与 hash。
3. **追加只读**：`BEFORE UPDATE/DELETE` 触发器 `RAISE(ABORT)`；`BEFORE INSERT` 校验前链头与 `entry_hash` 非空（密封段）。链头 `audit_chain` 与行插入同事务更新。
4. **覆盖范围只含安全元数据**：动作、决策、短原因、工具引用、matchKey、execHash、审批作用域、备份版本/hash/大小、链头。**不记录**文件正文、工具参数原文、模型提示词、密钥、MCP 返回正文。
5. **验证与导出**：`Verify` 流式复算，区分完整/破损/遗留未密封段，返回首个坏行；`Export` 生成 JSONL + manifest（含链头与验证结果），目录 0700、文件 0600。
6. **遗留行不伪造密封**：Phase 4 前的旧行 `entry_hash=''` 只允许位于首个密封行之前；密封段内出现空 hash 即视为破损。空库/仅旧行时报告"历史未密封"。
7. **放行类审计 fail-closed**：auto allow、审批 allow、持久规则命中在动作前写审计；写失败则拒绝执行并上屏。拒绝类事件写失败仅告警（动作已阻断）。
8. **无外部锚定**：不引入远程日志、WORM、外部时间戳。威胁模型明确：能检测单行改写、删除中间行、插队、链头不一致；不能检测拥有完整 DB 写权限者重算整链或全量截断。

## 理由

- 审计是安全决策的"黑匣子"；默认开启且不可关，才能让"为什么放行"在事后可复核。
- 复用 JCS 避免 canonical JSON 歧义；SHA-256 + prev 链是本地最低成本、可离线验证的方案。
- DB 触发器阻断误改与 naive 插入，比"只在应用层不提供改接口"更实在；同时承认它挡不住同权限攻击者。
- 只记元数据可避免审计本身成为敏感内容泄漏面；导出给用户/审计者保留证据。
- 外部锚定会引入网络、密钥或第三方依赖，超出本地优先与 Phase 4 范围；先做可验证的本地链。

## 后果

- 积极：安全事件可追溯；局部篡改可定位；导出可复算；不新增运行时依赖。
- 代价：每次放行多一次事务写入；审计日志只增不减，需要导出/归档；DB 故障时放行会被阻断（需可读错误与恢复指引）。
- 限制：全权限攻击者可重写整链；UI 与文档不得使用"不可篡改/不可删除"的绝对表述，应写"追加只读 + 可检测局部篡改"。
- 重新评估条件：出现合规/多用户/远程审计需求，或需要对抗本机全权限攻击者时，另立 ADR 评估外部锚定、OS 密钥链 HMAC 或 WORM 存储。
