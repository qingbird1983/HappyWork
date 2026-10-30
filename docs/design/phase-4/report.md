# Phase 4 验收报告：不可变审计 + 备份式回滚 + 三平台签名 CI

- 状态：实现完成，本地门禁全绿，分支已推送；A5 真机与 A6 远端 dry-run 待执行；`v*` 发版按所有者决定另排
- 日期：2026-10-30
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
| A5 UI/API | 部分：自动化通过，真机待验 | 前端 `render.test.tsx` 21 用例全绿，含新增审计/备份面板注入用例（恶意 `decision` 只能落到 `other` 类名、路径与原因转义、破损横幅显示首个坏行、运行中恢复按钮 `disabled`、二次确认文案）；`npm run lint/typecheck/format:check/test:unit/build` 全绿，App.tsx 568 行（<600）。**待所有者真机验收**：面板打开、链状态与导出文件、恢复/撤销交互与运行中提示 |
| A6 签名 dry-run | 待远端执行 | `release.yml` 已实现且通过 YAML 解析（5 个 job）；命令与任务名以本机 `wails3 task --list-all`、`wails3 tool sign -help`、Wails v3.0.0 `internal/commands/sign.go` 为准。**必须在推送后由 `workflow_dispatch` 实跑**：三平台构建 + 临时凭据签名 + 验签全绿才算通过；本次提交前无法给出运行证据，故不标注为通过 |
| A7 真实签名（条件） | 未执行（条件遗留） | 需要所有者提供 Windows Authenticode 证书、Apple Developer ID + 公证账号、Linux GPG 私钥与密钥 ID 并配置为 GitHub Actions Secrets。未提供前只记"条件未执行"，不伪造证据 |
| A8 门禁 | 本地全绿，远端待 CI | 见 §3 |

## 3. 本地门禁结果（2026-10-30）

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
| 远端 CI | `ci.yml` run `41094526014`、`41096410911`（分支 `feat/phase4-audit-rollback-signing`） | 两次首跑都只红在 `check`，三平台 `build` job 均通过。第一次：审计导出含工作区绝对路径（§4.8，P-013）；第二次：`AppendAudit`/备份写入在并发下报 `database is locked`（§4.9，P-014）。两处都已修复并补回归测试，待第三次复跑确认 |

## 4. 实现期间的发现与处置

1. **构建资产与配置脱节**：`build/linux/nfpm/nfpm.yaml`、`build/windows/info.json`、`build/windows/nsis/wails_tools.nsh`、`build/darwin/Info.plist` 仍是 `wails3 init` 模板值（`happywork.exe`、"My Company"、0.1.0），与 `build/config.yml` 的 0.0.1 不一致。已运行 `wails3 task common:update:build-assets` 重新生成并入库，同时把 `scripts/check-version.sh` 扩成"配置 + 生成资产"双校验：只校验配置会让发布物自报错误版本。`build/linux/nfpm/nfpm.yaml` 的 `homepage` 已从 `wails.io` 改为项目仓库地址。
2. **产物名是小写项目名**：实测 `wails3 build` 产出 `bin/happywork.exe`，NSIS 产出 `bin/happywork-amd64-installer.exe`（`INFO_PROJECTNAME` + `ARCH`），并非设计稿里写的 `HappyWork.*`。workflow 与文档统一改为小写并对安装包/AppImage 使用通配匹配 + 缺失即失败。
3. **`wails3 tool sign` 的参数语义**：Linux 的 `--pgp-key` 传给 `dpkg-sig -k`，是**密钥 ID**而非私钥文件路径（读 Wails v3.0.0 源码确认）；`rpmsign`/`rpm --checksig` 依赖 `~/.rpmmacros` 的 `%_gpg_name`，且 `rpm --checksig` 需要先导入公钥，否则只会报 `digests OK`。因此 workflow 写 `rpmmacros`、导入发布公钥，并把断言收紧到 `digests signatures OK`。
4. **dry-run 不能假装"已签名"**：macOS 没有 Developer ID 身份时 DMG 无法签名，故 DMG 的 `codesign`/`spctl`/`stapler` 校验只在正式标签路径执行；Windows dry-run 仍走真实 `signtool verify /pa /all`（临时自签证书装入当前用户受信任根），保证验签链路真的跑过。
5. **`workflow_dispatch` 的 `dry_run` 输入被去掉**：手动触发没有标签就没有"正式"语义，保留一个不生效的输入只会误导，因此 dispatch 一律 dry-run，`release` job 条件为 `github.event_name == 'push'`，产物名用 `release` / `dry-run-preview` 后缀隔离。
6. **GPG 口令卫生**：口令写入 `0600` 临时文件后用 `--passphrase-file` 传给 `gpg`；`wails3 tool sign --pgp-password` 只能收字符串，故从该文件读出后传入，不在任何 `echo` 中出现。已知限制：Wails 内部把该口令作为 `dpkg-sig --gpg-options` 参数传入，会短暂出现在子进程命令行中（ephemeral runner 上可接受，上游行为，不掩盖）。
7. **审批链路拆分**：`askApproval` 从 `exec.go` 移到 `approval.go` 以守住 600 行硬门槛，无行为变更；审计写入改用 `auditApproval`/`auditPermissionRule` helper，消掉重复构造。
8. **首跑 CI 抓到审计证据泄漏工作区绝对路径（仅 Linux 复现）**：`tool_call`/`approval`/`permission_rule` 三类记录把 `matchKey` 原样写进 `detail`，而 `permission.WriteMatchKey` 里含**绝对父目录**；导出 JSONL 后用户的目录结构随证据外流，违反 D2。本地之所以全绿有两层原因——Windows 上 matchKey 被 `strings.ToLower` 处理过，而测试用 `strings.Contains(jsonl, root.String())` 做**字节精确**比较；即便大小写对齐，路径还躺在 JSON 字符串字段里被多一层反斜杠转义。处置分两步：先把测试改成"去掉分隔符 + 忽略大小写"的比较，让它在本机也红（复现测试先于修复，standards/05 §1）；再从源头修——新增 `Session.auditKey/auditPath`，写进审计的目录一律工作区相对（根内为 `.` 或 `src/cmd`，根外为 `<outside>`），MCP 等无路径键原样透传，数据库里用于匹配的 `permission_rules.match_key` 保持绝对（证据链与匹配键分离）。修复后 `TestPhase4AuditKeyKeepsEvidenceWorkspaceRelative` 固化平台差异。

9. **第二次 CI 失败暴露 SQLite 连接层缺陷（同样只在远端复现）**：`check` job 报 `database is locked (517)` 与 `rollback: mark undo: database is locked (5)`。根因是三层叠加——① `sql.DB` 池未设上限，而 SQLite 的 `busy_timeout/WAL/foreign_keys` 是**按连接**生效的，pragma 只落在第一条连接上；② `AppendAudit` 的延迟事务先读链头再写，属于读→写升级，遇到别的连接已提交会立刻返回 `SQLITE_BUSY_SNAPSHOT`(517)，`busy_timeout` 不重试；③ `AppendMessage` 的"先查 `MAX(seq)` 再插入"和备份版本分配不在同一事务，交错时撞 `UNIQUE(session_id, seq)`。处置：`SetMaxOpenConns(1)`（单用户桌面库，串行比 WAL 并发更有价值，注释已写取舍），新增 `internal/storage/sqlite_write.go` 用 `writeTx` 把序号/版本分配并入插入事务，并补 `sqlite_contention_test.go`（4 写者 × 4 读者 × 25 轮）——修前本机三条症状全部复现，修后 `-count=3` 稳定绿。教训见 P-014；这也说明 P-013 的"本地全绿≠跨平台全绿"同样适用于并发路径。

## 5. 遗留与后续

- **A5 真机验收**：所有者在隔离 `APPDATA` 下走一遍——写文件审批→审计面板看链状态→导出 JSONL/manifest→备份面板恢复与撤销创建→运行中尝试恢复看提示。
- **A6 远端 dry-run**：分支已推送，手动触发 `Release` workflow，三平台 job 全绿才算通过；首次实跑大概率要按日志微调（`choco install nsis`、`dpkg-sig` 行为、AppImage 文件名大小写）。dry-run 产物只标 `dry-run-preview`，不构成发布。
- **发版排期（所有者 2026-10-31 决定）**：UI 细设计与打磨未完成，本期**不打 `v*` 标签、不发版**；首发平台 Windows 优先，macOS/Linux 的验收标准只是"可构建、可签名、可验签"，不承诺首发打磨度。
- **A7 真实签名**：随上面的排期决定主动延后；等凭据到位后补跑并回填证据（证书主体、GPG 指纹）。
- **不自动改写构建资产**：`check-version.sh` 只检测不修复，版本变更必须是一次显式提交。
- **备份孤儿**：`file_backups` 元数据丢失时磁盘版本文件不会被扫描重建（设计 §10 已记为风险）。
- **未新增依赖**：签名与打包全部走 Wails 现有 CLI 与平台工具；Go 运行时依赖未变。
