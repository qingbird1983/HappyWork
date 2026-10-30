# ADR-0014：内容信任级作为条件维度，不并入 matchKey

- 状态：定稿（2026-10-30，随 Phase 3 立项放行）
- 日期：2026-10-30
- 关联：[architecture/03](../architecture/03-permission-and-approval.md) §风险与待决；[ADR-0003](ADR-0003-dual-fingerprint.md)；Phase 2 报告（MCP untrusted 标记）

## 背景

architecture/03 留待决：matchKey 是否纳入"内容信任级"（外部内容触发的写操作自动降级为 once）。Phase 2 已把 MCP 结果标记为 `untrusted`，但尚未影响作用域；提示注入可借已存在的 session 授权，让"读外部内容 → 写文件"在无新弹窗的情况下发生。

## 决策

1. 内容信任级是 **DIM3 条件**，不进入 matchKey。
2. 每个 run 维护一个"已见不可信内容"闩锁：一旦当前 run 的上下文包含 `trust=untrusted` 的工具结果，直至下一次用户提交前保持为真。
3. 闩锁为真时，所有**副作用**审批（write / delete / execute / network）强制 `once`：忽略 session 授权与持久规则；弹窗标注"由外部内容触发，仅本次"。只读自动放行不受影响，敏感路径硬拒不变。
4. `approvals.content_trust` 记录决策时的信任级；审计与事件同步。闩锁在新 run 开始时重置。

## 理由

- 若把信任级并入 matchKey，用户在"可信上下文"下创建的 session 授权会在外部内容出现后继续命中（或产生平行授权），正是要防的路径。
- 条件降级只增加一次点击（每个不可信 run 最多一次），远低于把写操作全部改成逐条 Block 的疲劳成本。
- 不改 matchKey 结构，避免已落库授权语义迁移；未来可把闩锁升级为更细的来源分级，而不动指纹。

## 后果

- 积极：阻断"外部内容 + 已有授权 = 静默写入"的组合；安全语义可在 UI 解释。
- 代价：MCP 读写混合任务多一次确认；纯外部内容 workflow 的审批次数上升。
- 重新评估条件：出现可靠的"内容可信度分级/来源签名"后，可用更细粒度条件替代布尔闩锁。
