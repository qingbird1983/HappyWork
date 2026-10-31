# Phase 4 验收报告：不可变审计 + 备份式回滚 + 三平台签名 CI

- 状态：**Phase 4 验收完成（tag `phase-4.end`）**：A1~A6、A8 通过（A5 为所有者授权的代跑，2026-10-31）；A7 条件项随首发延后；macOS/Linux 只保证"可构建 + 可签名 + 可验签"；`v*` 发版按所有者决定另排
- 日期：2026-10-30 初稿；2026-10-31 更新 A6 证据与 A5 代跑结果
- 范围合同：[phase-4-audit-rollback-signing.md](../../roadmap/phase-4-audit-rollback-signing.md)
- 设计与决策：[design.md](design.md)、[release-signing.md](release-signing.md)、[ADR-0015](../../adr/ADR-0015-immutable-audit-chain.md)、[ADR-0016](../../adr/ADR-0016-versioned-backup-restore.md)、[ADR-0017](../../adr/ADR-0017-release-signing.md)

## 1. 交付内容

| 块 | 落位 | 说明 |
|---|---|---|
| 迁移 0004 | `migrations/0004_phase4.sql` | 审计表加 `turn_id/trace_id/detail_json/prev_hash/entry_hash`；新增 `audit_chain` 链头表与 `file_backups`；`audit_log` 的 UPDATE/DELETE 触发器 `RAISE(ABORT)`，插入时校验前驱哈希 |
| 审计链 | `internal/audit/{audit,verify,export}.go` | RFC 8785 JCS + SHA-256 链式哈希、流式验证（破损定位/遗留段/链头一致性）、JSONL + manifest 导出（目录 0700、文件 0600） |
| 版本化备份 | `internal/rollback/{rollback,files,writer,restore}.go` | 覆盖写保留版本、每文件 20 版、备份区 512 MiB 软上限、恢复前安全副本、新建撤销移入 `discarded/`（不 unlink）、写后 SHA-256 复核与自动回滚 |
| 接线 | `internal/{storage,tool,agent,desktop}`、`cmd/agent` | 审计写入收敛为 `audit.Record`；trace/turn 贯穿；放行路径审计失败即拒绝（fail-closed）；`write_file` 委托 `rollback.Writer`；启动恢复与 Hook 结果写审计 |
| 桌面 API | `internal/desktop/service.go` | `ListAudit/VerifyAudit/ExportAudit/ListBackups/RestoreBackup/UndoCreatedFile` |
| 前端 | `frontend/src/components/{AuditPanel,BackupsPanel,SecurityPanel}.tsx` | 顶栏"审计 / 备份"抽屉：链状态徽章、破损横幅、验证/导出、版本列表与二次确认；全部文本经 `SafeText`，CSP 与渲染基线不变 |
| 发布流水线 | `.github/workflows/release.yml`、`scripts/check-version.sh` | `v*` 标签三平台签名与验签；`workflow_dispatch` 一律 dry-run；`release` job 汇总 `SHA256SUMS` 并签名后创建 GitHub Release |

## 2. 验收场景结果

| 场景 | 结果 | 证据 |
|---|---|---|
| A1 审计链 | 通过（自动化） | `TestAuditChainAndTriggers`、`TestPhase4MigrationLegacyAudit`（`internal/storage`）；`TestVerifyAcceptsChain`/`DetectsTamper`/`DetectsDeletion`/`DetectsUnsealedInsert`/`DetectsHeadMismatch`/`LegacyPrefix`/`LegacyOnly`、`TestHashDeterministicAndDetailObject`（`internal/audit`）：改行、删中间行、插队、空哈希插入、链头不一致均可定位首个坏行；UPDATE/DELETE 被触发器拒绝；重启续链由 `TestMigrateIdempotent` + 同一 store 二次追加覆盖 |
| A2 覆盖与导出 | 通过（自动化） | `TestPhase4AuditCoversWriteApprovalAndBackup`（判定 `tool_call/ask` + `approval/allow` + `backup/create` 同链）、`TestPhase4ExportAndVerifyChain`（导出行数与链一致、不含工作区绝对路径与文件正文）、`TestExportEvidence`（JSONL 复算 + manifest 一致）、`TestHookPatchesWriteAndAudits`、`TestBoundaryDenialAudited`、`TestPhase4AuditFailureBlocksAllow`（审计写失败时放行被拒） |
| A3 备份版本与恢复 | 通过（自动化 + 会话级集成） | `TestWriteCreateOverwriteVersions`、`TestWriteRetentionPrunes`、`TestWriteQuotaRefusesOverwrite`、`TestRestoreCreatesSafetyCopyAndCanBeUndone`、`TestRestoreRecreatesDeletedFile`、`TestFileBackupRoundtrip`；新增 `TestPhase4RestoreFlowWithSafetyCopy`（恢复→安全副本→再恢复回 v2，两次 `backup/restore` 审计）、`TestPhase4RestoreRejectedWhileRunning`（运行中 `ErrBusy`，空闲后成功） |
| A4 新建撤销与写后校验 | 通过（自动化） | `TestUndoCreate`、`TestUndoCreateRejectsModified`、`TestPhase4UndoCreatedFileMovesToBackupArea`（撤销后工作区文件消失、内容在 `backups/<session>/discarded/...trash` 可读、审计 `backup/undo_create`）、`TestVerifyFailureRollsBack`、`TestVerifyFailureOnCreateDiscards`（故障注入自动回滚/移入备份区并审计 `backup/verify_fail`）、`TestWriteRejectsSensitiveAndTraversal` |
| A5 UI/API | **通过（代跑，2026-10-31）** | 所有者授权后，在隔离数据目录 `D:\hw-a5\appdata` + 工作区 `D:\hw-a5\ws` 用真实 GUI 走完清单（`docs/design/phase-4/a5-checklist.md`）：① 审批弹窗显示相对路径 `note.txt`，内容预览 `第一版`；② 审计面板「链状态：完整 · 密封 17 条 · 遗留 0 条」，`验证整链` 提示通过；③ 三次导出（3/16/17 条）均无绝对路径、文件正文或盘符，manifest `ok/headMatch` 为真；④ 备份面板列出 note.txt 的 v1 新建 / v2 覆盖 / v3 安全副本 / v4 覆盖与 tmp.txt；恢复 v2 后内容回到「第一版」并新增安全副本；⑤ 撤销创建 tmp.txt 后工作区消失、`backups/<sessionID>/discarded/tmp.txt/…trash` 保留 13 B 原文，审计新增 `backup/undo_create`；⑥ 运行中提示为「会话正在运行；请先停止，再恢复或撤销创建。」且按钮置灰。**真机发现一个缺陷：安全副本显示「未确认」且没有恢复按钮（无法撤销恢复）**，已按 ADR-0016 修复并加前端回归测试（§4.19，P-020）；修复后实测恢复安全副本可把内容退回「第二版」。前端 22 用例全绿 |
| A6 签名 dry-run | **通过（Windows，run `41157581586`）**；macOS 远端早已绿；Linux 按首发范围延后 | 取证只能在默认分支触发（P-015）。前四次失败依次暴露并修复：证书存储挂死（§4.10，P-016）、`wails3 tool sign` 在 Windows 未实现（§4.13）、PowerShell 语法（§4.14）、尾部篡改的假负向对照（§4.15，P-017）、NSIS 的 PATH（§4.16，P-018）、`$LASTEXITCODE` 退出码（§4.17，P-019）。最终 run `41157581586`（main，`workflow_dispatch`，默认 `platforms=windows`）：`prepare` 2m09s 绿、`windows` 3m45s 绿、macOS/Linux/release 按门控跳过（§4.18）。日志证据：`makensis` 找到并生成 `happywork-amd64-installer.exe`；`signtool sign` 成功签 `bin\happywork.exe` 与安装包；两个产物 `Get-AuthenticodeSignature` 均为 `UnknownError` + 临时证书指纹 `EDE675E953E1AB27E4FF44E9155458CE5BEAA9F3` + 代码签名 EKU；负向对照两次 `verify exit=1` 且命中 `0x80096010`；产物 `windows-dry-run-preview.zip`（15,338,322 字节，ID 13026277031）上传成功。**验收范围随 ADR-0018 收窄为 Windows dry-run 三件事：构建、签名、验签——本次全部成立。** |
| A7 真实签名（条件） | 未执行（条件遗留） | 需要所有者提供 Windows Authenticode 证书、Apple Developer ID + 公证账号、Linux GPG 私钥与密钥 ID 并配置为 GitHub Actions Secrets。未提供前只记"条件未执行"，不伪造证据 |
| A8 门禁 | **通过（2026-10-31）** | 本地 `bash scripts/gate.sh --race` 全绿（fmt/build/vet/lint/test/前端四门/渲染安全/体量）；远端 `ci.yml` run `41098214658`、`41102545807` 全绿；首发平台 `wails3 build` 与 Windows release dry-run 全绿（run `41157581586`）；A5 代跑结果已回填本报告并定稿。看板 A8 里"release dry-run CI 全绿"按 ADR-0018 收敛为 **Windows 单平台 dry-run 绿**，其余平台延后不算失败 |

## 3. 本地门禁结果（2026-10-30 首次；2026-10-31 复跑见表末）

| 门禁 | 命令 | 结果 |
|---|---|---|
| 格式化 | `gofmt -l`（按 CI 语义先归一 CRLF） | 无输出 → 通过（本机 `core.autocrlf=true` 会让 `gofmt -l .` 误报历史文件，仓库内以 LF 存储，`.gitattributes` 兜底） |
| 静态检查 | `go vet ./...`；`golangci-lint run ./...` | 通过；`0 issues` |
| Go 测试 | `go test ./...` | 全部 `ok`（含 `internal/audit`、`internal/rollback` 新包） |
| 竞态 | `go test ./... -race` | 全部 `ok`；期间修掉一处**测试脚手架**数据竞争（`fakeProvider.calls` 无锁读，改为 `callCount()`），非产品代码缺陷 |
| 体量 | `bash scripts/check-size` | 全部通过；`internal/agent/exec.go` 因新增审计接线涨到 613 行，按 600 行硬门槛把审批链路拆到 `internal/agent/approval.go` |
| 渲染安全 | `bash scripts/check-render-safety.sh` | `ok` |
| 前端 | `npm run lint` / `typecheck` / `format:check` / `test:unit` / `build` | 全绿；21 用例通过；产物 `dist/assets/index-*.js` 270.31 kB |
| 版本门禁 | `bash scripts/check-version.sh v0.0.1` | 通过；`v9.9.9` 与 `phase-4.end` 分别按预期失败（退出码 1 与 2） |
| 桌面构建 | `wails3 build`（Windows 本机） | 通过，产物 `bin/happywork.exe`；三平台 `wails3 build` 由 CI 矩阵覆盖 |
| 远端 CI | `ci.yml` runs `41094526014`、`41096410911`、`41098214658`（分支 `feat/phase4-audit-rollback-signing`）+ main 上的 `41102545807` | 前两次都只红在 `check`，三平台 `build` job 均通过：第一次审计导出含工作区绝对路径（§4.8，P-013），第二次 `database is locked`（§4.9，P-014）。第三次 `41098214658` **全绿**（`check` + 三平台 `build`），合并 main 后的 `41102545807` 同样全绿。此后按 ADR-0018，`ci.yml` 不再每轮 push 触发 |
| 远端 Release dry-run | run `41102612818`：`prepare` 成功、`macos` 成功、`linux` 失败、`windows` 挂死被掐；run `41156332487`：红在 NSIS PATH（§4.16）；run `41156918894`：构建/签名/验签全过、步骤退出码 1（§4.17）；**run `41157581586`：全绿（`prepare` 2m09s、`windows` 3m45s、其余按 `platforms=windows` 跳过）** | 见 §4.10、§4.16–§4.18。Windows dry-run 的产物与日志已覆盖 A6 三件事；macOS 的 ad-hoc 签名 + 校验在 run `41102612818` 已有一次远端绿；Linux 按首发范围延后。成本：单平台一次约 6 分钟作业时间（Windows 2× 计费） |
| 同源门禁脚本 | `bash scripts/gate.sh`（`--race` / `--build` / `--skip-lint`），`ci.yml` 的 check job 现在调用同一脚本 `--skip-lint` | 全绿；本地与远端不再各写一份命令（ADR-0018） |
| 同源门禁复跑（2026-10-31） | `bash scripts/gate.sh --race` | 全绿：fmt/build/vet/lint/test(-race)/前端四门/渲染安全/体量；负向对照修复后重跑 |
| Windows 签名链路（本机） | `powershell -File scripts\windows-sign-check.ps1` | `PASS`：内存临时证书 → `signtool sign` 签**副本** → `Get-AuthenticodeSignature` 验签（指纹 + EKU，状态 `UnknownError` 属预期）→ 主体篡改被 `signtool verify` 以 `0x80096010` 拒绝；`bin\happywork.exe` 仍为 `NotSigned`，留给 A5 |

## 4. 实现期间的发现与处置

1. **构建资产与配置脱节**：`build/linux/nfpm/nfpm.yaml`、`build/windows/info.json`、`build/windows/nsis/wails_tools.nsh`、`build/darwin/Info.plist` 仍是 `wails3 init` 模板值（`happywork.exe`、"My Company"、0.1.0），与 `build/config.yml` 的 0.0.1 不一致。已运行 `wails3 task common:update:build-assets` 重新生成并入库，同时把 `scripts/check-version.sh` 扩成"配置 + 生成资产"双校验：只校验配置会让发布物自报错误版本。`build/linux/nfpm/nfpm.yaml` 的 `homepage` 已从 `wails.io` 改为项目仓库地址。
2. **产物名是小写项目名**：实测 `wails3 build` 产出 `bin/happywork.exe`，NSIS 产出 `bin/happywork-amd64-installer.exe`（`INFO_PROJECTNAME` + `ARCH`），并非设计稿里写的 `HappyWork.*`。workflow 与文档统一改为小写并对安装包/AppImage 使用通配匹配 + 缺失即失败。
3. **`wails3 tool sign` 的参数语义**：Linux 的 `--pgp-key` 传给 `dpkg-sig -k`，是**密钥 ID**而非私钥文件路径（读 Wails v3.0.0 源码确认）；`rpmsign`/`rpm --checksig` 依赖 `~/.rpmmacros` 的 `%_gpg_name`，且 `rpm --checksig` 需要先导入公钥，否则只会报 `digests OK`。因此 workflow 写 `rpmmacros`、导入发布公钥，并把断言收紧到 `digests signatures OK`。
4. **dry-run 不能假装"已签名"**：macOS 没有 Developer ID 身份时 DMG 无法签名，故 DMG 的 `codesign`/`spctl`/`stapler` 校验只在正式标签路径执行。Windows 侧原设计"把临时自签证书装进当前用户受信任根，再让 `signtool verify /pa /all` 走真链路"在远端实跑中被证伪：导入受信任根会弹系统确认框，无人值守 runner 上没点，job 静默挂死 63 分钟（§4.10）。现改为内存构造临时证书 + `Get-AuthenticodeSignature` 断言"签名存在（看 `SignerCertificate`）/签名者匹配/具备代码签名 EKU"，负向对照要求主体篡改报 `TRUST_E_BAD_DIGEST (0x80096010)`（§4.15）；`/pa` 真链路只保留给真实证书路径。
5. **`workflow_dispatch` 的 `dry_run` 输入被去掉**：手动触发没有标签就没有"正式"语义，保留一个不生效的输入只会误导，因此 dispatch 一律 dry-run，`release` job 条件为 `github.event_name == 'push'`，产物名用 `release` / `dry-run-preview` 后缀隔离。
6. **GPG 口令卫生**：口令写入 `0600` 临时文件后用 `--passphrase-file` 传给 `gpg`；`wails3 tool sign --pgp-password` 只能收字符串，故从该文件读出后传入，不在任何 `echo` 中出现。已知限制：Wails 内部把该口令作为 `dpkg-sig --gpg-options` 参数传入，会短暂出现在子进程命令行中（ephemeral runner 上可接受，上游行为，不掩盖）。
7. **审批链路拆分**：`askApproval` 从 `exec.go` 移到 `approval.go` 以守住 600 行硬门槛，无行为变更；审计写入改用 `auditApproval`/`auditPermissionRule` helper，消掉重复构造。
8. **首跑 CI 抓到审计证据泄漏工作区绝对路径（仅 Linux 复现）**：`tool_call`/`approval`/`permission_rule` 三类记录把 `matchKey` 原样写进 `detail`，而 `permission.WriteMatchKey` 里含**绝对父目录**；导出 JSONL 后用户的目录结构随证据外流，违反 D2。本地之所以全绿有两层原因——Windows 上 matchKey 被 `strings.ToLower` 处理过，而测试用 `strings.Contains(jsonl, root.String())` 做**字节精确**比较；即便大小写对齐，路径还躺在 JSON 字符串字段里被多一层反斜杠转义。处置分两步：先把测试改成"去掉分隔符 + 忽略大小写"的比较，让它在本机也红（复现测试先于修复，standards/05 §1）；再从源头修——新增 `Session.auditKey/auditPath`，写进审计的目录一律工作区相对（根内为 `.` 或 `src/cmd`，根外为 `<outside>`），MCP 等无路径键原样透传，数据库里用于匹配的 `permission_rules.match_key` 保持绝对（证据链与匹配键分离）。修复后 `TestPhase4AuditKeyKeepsEvidenceWorkspaceRelative` 固化平台差异。

9. **第二次 CI 失败暴露 SQLite 连接层缺陷（同样只在远端复现）**：`check` job 报 `database is locked (517)` 与 `rollback: mark undo: database is locked (5)`。根因是三层叠加——① `sql.DB` 池未设上限，而 SQLite 的 `busy_timeout/WAL/foreign_keys` 是**按连接**生效的，pragma 只落在第一条连接上；② `AppendAudit` 的延迟事务先读链头再写，属于读→写升级，遇到别的连接已提交会立刻返回 `SQLITE_BUSY_SNAPSHOT`(517)，`busy_timeout` 不重试；③ `AppendMessage` 的"先查 `MAX(seq)` 再插入"和备份版本分配不在同一事务，交错时撞 `UNIQUE(session_id, seq)`。处置：`SetMaxOpenConns(1)`（单用户桌面库，串行比 WAL 并发更有价值，注释已写取舍），新增 `internal/storage/sqlite_write.go` 用 `writeTx` 把序号/版本分配并入插入事务，并补 `sqlite_contention_test.go`（4 写者 × 4 读者 × 25 轮）——修前本机三条症状全部复现，修后 `-count=3` 稳定绿。教训见 P-014；这也说明 P-013 的"本地全绿≠跨平台全绿"同样适用于并发路径。

10. **A6 远端首跑：Windows job 静默挂死 63 分钟**（run `41102612818`，2026-10-31）。`prepare` 1.5 分钟、`macos` 106 秒成功、`linux` 20 秒失败（`E: Unable to locate package dpkg-sig`——`ubuntu-latest` 已是 Ubuntu 26.04，该包不可安装），`windows` 卡死在"准备签名证书"步骤直到被平台掐掉。按 Actions 倍率（Linux 1× / Windows 2× / macOS 10×）折算，这次运行约 **153 分钟额度**，当天 7 次运行合计约 **319 分钟**。根因与处置见 §4.4、P-016 与 [ADR-0018](../../adr/ADR-0018-local-first-gate-and-on-demand-ci.md)：所有 job 补 `timeout-minutes`，两个 workflow 加 `concurrency` + `cancel-in-progress`，`ci.yml` 取消 push 触发，`release.yml` 的 dry-run 默认只跑首发平台 `windows`，日常门禁改由本地同源脚本 `bash scripts/gate.sh` 承担。
11. **一处真实的事件顺序缺陷（本机并发才暴露）**：审批超时与预算暂停都先 `setState(StatePaused)` 再发 `EventPaused`，观察方能在"已暂停"状态下读不到暂停原因；并行负载下 `TestApprovalTimeoutPauseResume` 断到 `paused events = 0`。改为**先发事件再翻状态**（`resumeCh` 有 1 格缓冲，提前发事件不会丢 Resume），并在测试里固化顺序不变量：`EventPaused` 的索引必须早于 `EventState(paused)`。修复前该断言本机稳定红（idx 8 > idx 7），修复后绿。
12. **换行纪律补漏**：`.gitattributes` 原先只强制 `*.sh`/`scripts/*`/`migrations/*.sql` 为 LF，Windows 上 `core.autocrlf=true` 会把 go/ts/tsx/css/json 等 checkout 成 CRLF，导致本机 `prettier --check`、`gofmt -l` 全线误报并留下幻影 diff（P-013 里那个"临时拷出去再比"的绕法就是为它准备的）。现对这些扩展名统一 `eol=lf`，本地与 CI 看同一份字节。

13. **`wails3 tool sign` 在 Windows 上根本没有实现**（本机实测、零额度发现）：执行即报 `ERROR built-in Windows signing not yet implemented - please use signtool.exe on Windows, or install osslsigncode`。Wails v3.0.0 只实现了 macOS（codesign）与 Linux（dpkg-sig/rpmsign）两条签名路径，Windows 是空的——也就是说 **A6 的 Windows 部分在改成直接调用 `signtool` 之前不可能通过，远端跑多少次都没用**。这是 P-012（"help 文本不作为依据"）的第二击：上次错在参数语义，这次错在功能是否存在。
14. **Windows dry-run 第二次红在 PowerShell 语法**（run `41118979006`，166 秒即失败——超时与快速失败机制生效，不再挂一小时）：`New-Object Type(a, b)` 不是合法写法（`Missing ')' in method call`），且 `X500DistinguishedName` 的命名空间写错。处置：新增 `scripts/windows-sign-check.ps1`，在本机把"内存临时证书 → `signtool sign` → `Get-AuthenticodeSignature` 验签 → 篡改负向对照"整条链路跑通，再把同样的写法搬进 workflow，并用 PowerShell 的 AST 解析器对 5 个 `pwsh` 步骤逐一语法检查（全部 OK）。**"本地先跑通再推远端"从这一条起才算成立**——前两次远端失败本可在本机发现。
15. **负向对照的第一版是假的，本机复跑才暴露**：最初把文件**尾部**第 3 个字节翻转（实测状态 `NotSigned`），换一个新构建的 exe 后同一操作得到 `UnknownError`，旧断言判不出"篡改"与"证书不受信任"的区别。多位置实测（P-017）：改**镜像主体**（25%/60%）→ `HashMismatch` 且 `signtool verify` 报 `0x80096010`；改尾部 → 只是破坏 PKCS#7 证书表，退化成 `NotSigned`/链错误，摘要未被比对。最终判据改为"主体篡改 + `signtool` 必须报 `TRUST_E_BAD_DIGEST`"，错误码不受语言与 .NET 版本影响；本地脚本改正后整个链路在本机 5.1 下通过。顺带发现 5.1 按行读 UTF-8 文件必须 `-Encoding UTF8`（644 行被读成 594 行），否则 workflow 结构检查会整段错位（P-016 预防清单）。
16. **第三次 dry-run（run `41156332487`）：NSIS 装上了，但后续步骤 PATH 没刷新**。这次运行 5 分 36 秒结束（`prepare` 2m33s 绿、`windows` 2m50s 红、linux/macos 按 `platforms=windows` 跳过），失败点是 `windows:create:nsis:installer` 报 `"makensis": executable file not found in $PATH`；而前一步 `choco install nsis` 的日志明确写着部署到 `C:\Program Files (x86)\NSIS`。根因是 choco 改的是注册表 PATH，runner agent 的环境快照不会刷新（P-018）。修复：安装步骤显式定位 `makensis.exe` 并写入 `$GITHUB_PATH`，找不到即失败。这次失败也验证了新的成本防线——5 分钟显式失败，没有再挂一小时。
17. **第四次 dry-run（run `41156918894`）：签名与验签全部通过，步骤却以退出码 1 结束**。这次走到了最后：NSIS 生成 `happywork-amd64-installer.exe`、`signtool sign` 签了 exe 与安装包、`Get-AuthenticodeSignature` 在两个产物上都拿到 `UnknownError` + 临时证书指纹、负向对照两次都 `verify exit=1`，还打印了"dry-run 验签通过"。但步骤仍红在 `Process completed with exit code 1`——根因是 runner 用 `pwsh -command ". '{0}'"`，脚本正常结束时把最后一次原生命令（负向对照里按设计失败的 `signtool verify`）的退出码当成了进程退出码（P-019）。修复：在故意执行失败命令的脚本末尾显式 `exit 0`。**A6 的三件事（构建、签名、验签）在这一次的日志证据里都已成立，还差同一份修复后的一次绿 run 作为正式取证。**
18. **第五次 dry-run（run `41157581586`）全绿，A6 取证完成**。`prepare` 2m09s、`windows` 3m45s、`macos`/`linux`/`release` 按 `platforms=windows` 跳过，整 run `success`。关键日志：NSIS 安装后 `makensis` 可执行（`GITHUB_PATH` 生效）；`Successfully signed: bin\happywork.exe` 与 `Successfully signed: ...\happywork-amd64-installer.exe`；两个产物均 `status=UnknownError signer=EDE675E953E1AB27E4FF44E9155458CE5BEAA9F3`（临时证书指纹），EKU 断言通过；负向对照两次 `verify exit=1` 且脚本未报"缺 0x80096010"，即摘要错误被确认；产物 `windows-dry-run-preview.zip`（15,338,322 字节，Artifact ID 13026277031）上传成功。至此 A6 按 ADR-0018 收窄后的"构建、签名、验签"三件事全部有远端证据，且没有触发 `release` job（手动触发永不发布）。
19. **A5 代跑抓到安全副本不可恢复的 UI 缺陷**。所有者 2026-10-31 授权代跑后，在隔离目录走完清单。恢复 v2 后，安全副本 v3 在备份面板显示「未确认」且没有「恢复」按钮——而 ADR-0016 明确写着"恢复本身可再恢复/撤销"。根因：`makeBackup` 创建安全副本时没有写 `verified_at`（它本来就没有"写入后校验"这一步），前端 `canRestore` 只检查 `verified_at`，于是把安全副本判成不可用；后端 `Restore` 其实不要求该字段。处置：前端把 `reason=pre_restore` 且带 `backupRel` 的安全副本视为可恢复，并把标签改成「安全副本」；先加会红的前端用例（`offers the pre-restore safety copy as a restore source`，修复前实际渲染为「未确认」且无按钮），修后 22 个前端用例全绿。修复后用真实 GUI 再点一次安全副本的恢复：`note.txt` 从「第一版」退回「第二版」，原安全副本变为「已恢复/已撤销」，新安全副本带恢复按钮。另观察到：自动化鼠标点击确认按钮两次未触发、键盘激活正常（先记自动化坐标待复核）；模型在总结时误称 `note.txt` 为「第二版」（实际「第一版」，模型未重读文件，不是 UI 缺陷）。

## 5. 遗留与后续

- **A5 真机验收（已完成，代跑）**：2026-10-31 由 AI 在所有者授权下按 [a5-checklist.md](a5-checklist.md) 走完；结果见 §2 A5 与 §4.19。真机抓到的安全副本缺陷已修复并复验；可选的第 9 步（运行中强杀进程）未执行，留作回归时补。
- **A6 远端 dry-run（已完成）**：Windows 单平台 dry-run 在 run `41157581586` 全绿（§4.18），取证链路见 §2 A6 行。macOS 的 ad-hoc dry-run 在 run `41102612818` 已绿过一次；Linux 的 `dpkg-sig` 不可用与 macOS/Linux 的签名打磨按首发范围延后，不算失败。
- **A8 收尾（已完成）**：门禁项全绿（本地 `gate.sh --race`、`ci.yml`、Windows dry-run），A5 证据已回填本报告，看板 A5/A8 已勾选，tag `phase-4.end`（不打 `v*`）。
- **发版排期（所有者 2026-10-31 决定）**：UI 细设计与打磨未完成，本期**不打 `v*` 标签、不发版**；首发平台 Windows 优先，macOS/Linux 的验收标准只是"可构建、可签名、可验签"，不承诺首发打磨度。
- **A7 真实签名**：随上面的排期决定主动延后；等凭据到位后补跑并回填证据（证书主体、GPG 指纹）。
- **不自动改写构建资产**：`check-version.sh` 只检测不修复，版本变更必须是一次显式提交。
- **备份孤儿**：`file_backups` 元数据丢失时磁盘版本文件不会被扫描重建（设计 §10 已记为风险）。
- **未新增依赖**：签名与打包全部走 Wails 现有 CLI 与平台工具；Go 运行时依赖未变。
