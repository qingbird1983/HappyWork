# P-020 恢复前安全副本显示「未确认」且没有恢复按钮——恢复本身不可撤销

## 现象

A5 真机验收（2026-10-31，隔离数据目录）里：

1. 新建 `note.txt`（v1）→ 覆盖成第二版（v2 备份第一版）→ 在备份面板点 v2 的「恢复」；
2. 内容回到「第一版」，系统按设计新增一个「安全副本」（v3）；
3. 但 v3 的状态列显示 **未确认**，并且**没有「恢复」按钮**——用户无法用这个安全副本把内容退回「第二版」，恢复操作实际上不可撤销。

而 [ADR-0016](../../docs/adr/ADR-0016-versioned-backup-restore.md) 与 `a5-checklist.md` 都明确承诺"恢复本身可再恢复/撤销"。

## 根因

- 后端 `Restore` 在覆盖前调用 `makeBackup` 创建 `pre_restore` 副本；`makeBackup` 只记录 `backup_sha256`，**不写 `verified_at`**——因为安全副本没有"写入后重读校验"这一步（它拷贝的是当前已存在的内容，拷贝时已对副本重新计算哈希）。
- 前端 `canRestore` 只认 `verified_at` 非空，`backupLabel` 也把"覆盖但没有 verified_at"一律标成「未确认」，于是安全副本被 UI 判成不可用；后端 `Restore` 其实不要求该字段（恢复前会重新校验 `backup_sha256`）。
- 自动化测试覆盖了后端"安全副本可以再恢复"（`TestRestoreCreatesSafetyCopyAndCanBeUndone`），但**没有覆盖 UI 的可用性判据**；渲染测试只覆盖了普通未校验覆盖版本，所以两边各自绿、合起来不可用。

## 修复

- 前端 `BackupsPanel.tsx`：`pre_restore` 且带 `backupRel` 的副本视为可恢复（`canRestore` 放行），标签显示「安全副本」而不是「未确认」；普通未校验覆盖仍保持「未确认」且不提供按钮。
- 先写会红的前端用例 `offers the pre-restore safety copy as a restore source`：修复前实际渲染为「未确认」、没有按钮，测试失败；修复后 22 个前端用例全绿。
- 真机复验：点安全副本的恢复后，`note.txt` 从「第一版」退回「第二版」，原安全副本变「已恢复/已撤销」，并生成新的安全副本（可继续撤销）。

## 预防

- 后端语义与 UI 判据要成对写测试：凡是"某类记录可执行某操作"的领域规则，既要有后端测试，也要有渲染/交互测试，否则会出现"API 能做、界面不给做"。
- 用**真机验收**兜底：自动化测试通过不代表用户点得到；A5 这类清单必须在真实 UI 上逐条走。
- 状态词（「未确认」）必须对应明确的领域含义；如果某类记录天然没有该字段，UI 要有专门的判据与标签，不要复用相邻状态的措辞。

## 归属

Phase 4 A5 真机验收（2026-10-31）/ `frontend/src/components/BackupsPanel.tsx`、`frontend/src/render.test.tsx`、`docs/design/phase-4/a5-checklist.md`；与 P-013（本地绿但远端红）同族：缺少跨层验证。
