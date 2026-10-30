# ADR-0005：MVP 安全降级路线——备份式回滚、沙箱分期、外部 Hook 延后

- 状态：定稿
- 日期：2026-10-30
- 关联：[research/01](../research/01-v2-design-review.md) B2/B3/结论 3；[architecture/04](../architecture/04-security-and-hooks.md)

## 背景

v2 的阶段 3（2 周）承诺：三平台 OS 沙箱 + 文件系统快照 + 不可变审计 + 三平台签名 CI。核实事实：三平台沙箱能力差异极大（Windows AppContainer 需 profile SID + 目录 ACL，OpenAI 为此单独发表工程博客）；快照机制（VSS / APFS / btrfs）互不可移植且 APFS 快照需管理员权限；同类产品（Codex、Claude Code）均不做快照，靠沙箱 + 审批兜底。原工期不可信，且部分承诺不可兑现。

## 决策

1. **回滚降级为文件级备份**（覆盖写 = 临时文件 + 原子 rename + 原文件副本；删除 = 一律进 OS 回收站；git 仓库工作区提示用 git 兜底）。完整快照进 Backlog，立项须 ADR。
2. **沙箱分期**：Phase 1–4 无 OS 沙箱，靠权限引擎 + 审批 + 备份兜底；Phase 5 落地 macOS Seatbelt + Linux Landlock；Windows AppContainer 为 Backlog 独立里程碑（须独立 ADR + 安全评审）。
3. **外部进程 Hook 延后**出首版路线（v2 排在阶段 2）：进程内 Hook 为 MVP 唯一形态；外部形态（进程隔离 + JSON-RPC + 资源限制）后续立项。
4. 原"阶段 3"拆为 **Phase 4（审计 + 备份回滚 + 三平台签名 CI）** 与 **Phase 5（OS 沙箱）** 两个独立里程碑，各自独立验收。

## 理由

- 降级后的组合（权限 + 审批 + 备份 + 回收站）覆盖本地可逆操作的现实风险；不可逆操作本就不靠快照救（v2 自己的可逆性原则）。
- 分期使每期工程量可信、可验收；Codex 先例证明 Seatbelt/Landlock 先行、Windows 缓行是可行路径。

## 后果

- 积极：路线图工期可信；承诺均可兑现；Windows 用户提前获得除沙箱外的全部防线。
- 代价/义务：Phase 1–4 无沙箱期内本地破坏靠三重兜底（须在 README 明示风险边界）；备份目录需容量上限与清理策略。
- 重新评估条件：无沙箱期出现实际破坏事故，或 Seatbelt/Landlock 实现遇阻。
