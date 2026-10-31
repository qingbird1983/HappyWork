# Phase 4：不可变审计、备份式回滚与三平台签名

- 状态：定稿（2026-10-30，所有者放行 D1~D6）
- 日期：2026-10-30
- 关联：[00-roadmap](00-roadmap.md) §1；[architecture/02](../architecture/02-core-loop-and-data.md)、[04](../architecture/04-security-and-hooks.md)；[ADR-0015](../adr/ADR-0015-immutable-audit-chain.md)、[ADR-0016](../adr/ADR-0016-versioned-backup-restore.md)、[ADR-0017](../adr/ADR-0017-release-signing.md)；施工设计 [design.md](../design/phase-4/design.md)、[release-signing.md](../design/phase-4/release-signing.md)

## 1. 目标

Phase 3 证明"控制是确定、可扩展、可追溯的"；Phase 4 证明"**犯错的代价可追溯、可恢复，发布物可验证**"。

一句话验收：每一次安全判定与执行都能被链式校验；每一个被覆盖的文件都能回到任一保留版本；每一个正式发布物都能被平台工具验签。

## 1.1 需所有者放行的决策（D1~D6）

| # | 决策 | 推荐结论 | 一句话理由 |
|---|---|---|---|
| D1 | 审计链是否可选 | **默认开启，不提供关闭开关**；DB 触发器禁改禁删；本地威胁模型，不做外部锚定 | 可关闭的审计等于没有审计；单机收益在防误改/局部篡改，不在对抗全权限攻击者 |
| D2 | 审计覆盖面与隐私 | 只记录安全事件与元数据（判定、审批、Hook、备份/恢复、链校验）；**不记文件正文、提示词、密钥原文**；JSONL 导出附带链头与校验结果 | 审计要能回答"谁在何时允许了什么"，不是复制 transcript |
| D3 | 备份保留与恢复语义 | 每文件保留最近 **20** 个版本；备份区软上限 **512 MiB**；恢复前先备份当前状态；新建文件可"撤销创建"（移入备份区，不直接删除） | 覆盖写可回退，新建也可撤回；容量有界 |
| D4 | 写后校验失败怎么办 | 写入后重读并比对 SHA-256；失败时**自动回滚到写前版本**（新建则移入备份区）并审计 | 把"写完就算成功"升级为"写完且内容一致才算成功" |
| D5 | 签名凭据与验收方式 | `v*` 标签触发发布流水线；Windows Authenticode / macOS Developer ID + 公证 / Linux GPG；凭据**只存 GitHub Actions Secrets**；本阶段以 **dry-run（临时测试凭据）** 验收，真实签名作为**条件验收 A7** | 没有证书也能把流水线做对；真实签名需要所有者提供凭据，不能假装完成 |
| D6 | 本阶段 UI 范围 | 最小审计面板（最近事件、链状态、验证、导出）+ 备份面板（文件/版本、恢复、撤销创建）；不做更新器、搜索、批量恢复、完整快照 | 信息完整性优先，视觉精修仍留 Backlog |

> D5 是唯一外部依赖：若所有者无法提供真实签名凭据，A7 记为"条件未执行/遗留"，不阻塞 A1~A6、A8 与阶段收尾。

## 2. 范围

### 做

- **不可变审计**：`audit_log` 增加 `turn_id/trace_id/detail_json/prev_hash/entry_hash`；链式哈希、追加只读触发器、启动恢复事件、链验证与 JSONL 导出。
- **审计覆盖**：工具判定（auto/allow/ask/deny）、审批决策、持久规则创建/命中/删除、Hook 事件、备份创建/恢复/撤销/校验失败、审计链验证。所有记录只含引用、hash 与短原因。
- **备份版本化**：覆盖写按文件保存多版本（路径、版本号、SHA-256、大小、权限位、时间、工具调用关联）；保留策略与全备份区软上限。
- **恢复与撤销**：按版本恢复；恢复前自动备份当前状态，恢复本身可再撤销；新建文件未修改时可撤销创建（移动到备份区，不永久删除）。
- **写后校验**：写入后重读计算 SHA-256；不一致自动回滚并返回明确错误；备份副本写入前也校验 hash。
- **三平台发布签名流水线**：`v*` 标签触发；Windows Authenticode 签名 exe 与安装包；macOS Developer ID 签名、公证、staple；Linux `.deb/.rpm` GPG 签名 + `SHA256SUMS` 签名；产物上传 GitHub Release。
- **签名 dry-run**：`workflow_dispatch` 使用临时测试凭据在 CI 内完成签名与验签（macOS 为 ad-hoc + `codesign --verify`），不创建 Release。
- **迁移 0004**：审计链字段/触发器/`audit_chain`、`file_backups`。
- **前端**：审计面板、备份面板、恢复/撤销交互与错误提示；纯文本渲染与 CSP 不变。
- **测试**：链哈希/篡改/删除/遗留段、触发器、导出、备份多版本/保留/恢复/撤销/容量、写后校验故障注入、迁移升级、发布 workflow dry-run。

### 不做（明确排除）

- 完整文件系统快照、增量备份、跨设备同步、批量恢复整个会话。
- 远程审计锚定、外部时间戳、WORM 存储、审计全文检索或自动删除（追加只读，不设保留删除）。
- 应用内更新检查/下载器/静默自动更新；本阶段只做**发布物签名与验证**。
- Windows AppContainer、macOS Seatbelt、Linux Landlock（Phase 5）。
- 删除工具与 OS 回收站集成（当前无删除工具；新建撤销只移入应用备份区）。
- 证书采购、Apple 开发者账号注册、硬件令牌/EV 证书运维；属所有者外部准备。
- MSIX、多签名身份、密钥轮换 UI、Sigstore/透明日志（Backlog，须 ADR）。
- UI/UX 精修、Markdown 渲染、审计可视化图表。

## 3. 开工前置条件

- [x] Phase 3 完成（tag `phase-3.end`，main CI 绿）
- [ ] 本文件、design.md、release-signing.md 与 ADR-0015/0016/0017 经所有者过目
- [ ] 所有者确认 D5 采用 dry-run 验收，或提供三类真实签名凭据
- [ ] 无新增 Go 运行时依赖；若引入，先登记 [standards/04](../standards/04-coding.md) 附录 A
- [ ] 在 `feat/phase4-audit-rollback-signing` 分支开工

## 4. 交付物

| 类别 | 清单 |
|---|---|
| 代码 | `internal/audit/`（记录、链哈希、验证、导出）、`internal/rollback/`（版本、写后校验、恢复、撤销、保留/容量）、`internal/storage/`（迁移 0004、审计链追加、备份元数据）、`internal/agent/`（审计覆盖、trace、备份接线）、`internal/desktop/`（审计/备份 API）、`frontend/`（两个面板） |
| 文档 | 本文件、`design/phase-4/{design,release-signing}.md`、ADR-0015/0016/0017、`report.md`（验收后） |
| CI | `.github/workflows/release.yml`（`v*` 与 dry-run）、签名/验签脚本或 Taskfile 目标 |
| 其他 | 迁移 0004；故障注入与迁移测试；验收报告；architecture/02、04 回填 |

## 5. 验收场景

- [x] A1 审计链：新行带 `prev_hash/entry_hash`；任一行被改、删、插队可定位到首个坏行；UPDATE/DELETE 被触发器拒绝；验证区分完整、破损、遗留未密封段；重启后继续链接。
- [x] A2 覆盖与导出：工具判定、审批、持久规则、Hook、备份/恢复、链验证均有审计；不含正文/提示词/密钥原文；JSONL + manifest 导出可在测试中复算通过。
- [x] A3 备份版本：同一文件连续覆盖保留多版本（版本号/哈希/大小/权限/时间正确）；按保留策略裁剪；恢复所选版本成功，恢复前自动备份当前状态，恢复后可再恢复；越界/敏感/身份变化路径被拒绝。
- [x] A4 新建撤销与写后校验：新建文件记录 `kind=create`；未修改时可撤销创建并移入备份区；修改后拒绝撤销；写后 SHA-256 一致才成功；故障注入触发自动回滚/移入备份区并审计；备份区软上限触发时拒绝新的覆盖备份（新建不受影响）而非静默丢弃。
- [x] A5 UI/API：审计面板显示最近事件、链状态，可验证/导出；备份面板按文件列版本与大小，可恢复/撤销；运行中会话拒绝恢复并给出明确提示；渲染安全门禁全绿。真机操作步骤见 [design/phase-4/a5-checklist.md](../design/phase-4/a5-checklist.md)。**2026-10-31 所有者授权代跑完成**：链状态「完整/密封 17 条」，三次导出均无绝对路径/正文/盘符，恢复与撤销创建按预期工作，运行中提示「会话正在运行；请先停止，再恢复或撤销创建。」；真机抓到安全副本不可恢复的 UI 缺陷并修复（P-020），复验通过。
- [x] A6 签名 dry-run（**范围按 ADR-0018 收敛为 Windows**）：`release.yml` 的 dry-run 在 Windows 上完成"构建 → 用临时测试证书签名 → 验签"三件事且全绿，不上传 Release；缺少真实凭据时正式标签**明确失败**而不是发布未签名产物。取证依赖：`workflow_dispatch` 只对默认分支上存在的 workflow 生效，故只能在 `release.yml` 合入 main 之后触发（P-015）。**通过的 run：`41157581586`**（`prepare` 2m09s、`windows` 3m45s、macOS/Linux/release 按 `platforms=windows` 跳过；`makensis` 生成安装包、`signtool sign` 签 exe 与安装包、`Get-AuthenticodeSignature` 指纹 + EKU 断言、主体篡改报 `0x80096010`、产物 `windows-dry-run-preview.zip` 上传）。此前失败依次暴露 P-016/P-012 复发/P-018/P-019 与 PowerShell 语法问题，均已修复。macOS/Linux 按所有者"首发 Windows 优先"的决定延后，登记 backlog。
- [ ] A7 真实签名（条件，**已随首发排期主动延后**）：所有者提供 Windows 证书、Apple Developer ID/公证凭据、Linux GPG 私钥后，`v*` 标签产物通过 `signtool verify`、`codesign/spctl/stapler`、包签名平台工具（`dpkg-sig`/`rpm --checksig`）与 `gpg --verify SHA256SUMS.asc`；未提供则记录遗留，不伪造证据。
- [x] A8 门禁：审计/备份/迁移/故障注入测试全绿；`bash scripts/gate.sh --race` 全绿（fmt/build/vet/lint/test + 前端 format/lint/typecheck/unit + render-safety + check-size，本地与 CI 同源）；首发平台 `wails3 build` 与 Windows release dry-run 全绿（run `41157581586`，2026-10-31）；验收报告落盘并定稿。三平台齐跑与矩阵绿属于里程碑/正式标签要求，不再作为每轮推送的门禁（ADR-0018）。

## 6. 依赖与风险

| 依赖/风险 | 说明与缓解 |
|---|---|
| 审计日志增长 | 只记安全事件与元数据；UI 分页；导出后可由用户归档；不在本阶段自动删除（与追加只读冲突） |
| 无外部锚定 | 本地全权限攻击者可重写整条链；明示为"防误改/局部篡改"，不是对抗性证明；未来可评估远程锚定/OS 密钥链 HMAC |
| 触发器可被绕过 | 同权限攻击者可删触发器或直接改库；链校验仍能发现局部不一致；威胁模型写清 |
| 备份区膨胀 | 每文件 20 版 + 全局 512 MiB 软上限；超限拒绝新的覆盖备份/恢复安全副本并提示清理；新建不受影响；恢复也会产生安全副本 |
| 恢复与运行并发 | 恢复/撤销仅在会话非运行态允许；路径重新规范化并拒绝敏感/越界/身份变化 |
| 写后校验误报 | 仅比对写入内容的 SHA-256 与重读结果；故障注入测试覆盖；失败自动回滚并保留证据 |
| 签名凭据缺失 | dry-run 验收流水线；真实签名 A7 条件项；文档明确未签名产物不得作为正式发布 |
| 公证耗时/凭据轮换 | CI 超时与失败重试策略；凭据仅存 Secrets；轮换步骤写入 release-signing.md |
| 构建不可复现 | 本阶段不承诺 bit-for-bit 复现；以标签、CI 日志、产物哈希与签名共同追溯 |

## 7. 与前后阶段的边界

- **承接 Phase 3**：权限判定、Hook、execHash/TOCTOU、持久规则语义不变；Phase 4 只是把判定与副作用写进可验证审计，并补齐备份/恢复。
- **交给 Phase 5**：路径二次校验与执行之间仍有残窗；OS 沙箱落地后关闭。Windows AppContainer 仍为 Backlog。
- **发布边界**：本阶段完成签名与验证；应用内更新检查、下载页与版本提示不在范围内，避免网络面与自动更新风险提前引入。
