# Phase 4 施工设计：审计链、备份回滚与发布签名

- 状态：定稿（2026-10-30，所有者放行 D1~D6）
- 日期：2026-10-30
- 关联：[phase-4-audit-rollback-signing.md](../../roadmap/phase-4-audit-rollback-signing.md)（范围与 A1~A8）；[architecture/02](../../architecture/02-core-loop-and-data.md)、[04](../../architecture/04-security-and-hooks.md)；[ADR-0015](../../adr/ADR-0015-immutable-audit-chain.md)、[ADR-0016](../../adr/ADR-0016-versioned-backup-restore.md)、[ADR-0017](../../adr/ADR-0017-release-signing.md)；发布签名细节见 [release-signing.md](release-signing.md)

## 1. 目标与非目标

把 Phase 3 的"控制可追溯"升级为**可验证、可恢复、可发布**：

1. 审计**追加只读 + 哈希链**，能定位被改/被删/被插队的行；安全事件全覆盖，且不复制正文。
2. 文件覆盖保留**多版本**，恢复前先留当前状态；新建文件可撤销创建；写后 SHA-256 复核，失败自动回滚。
3. 发布流水线在 `v*` 标签上完成三平台签名与验签；无凭据时**明确失败**，不发布未签名产物。

非目标与范围合同一致：完整快照、远程锚定、更新器、删除工具、OS 沙箱、批量恢复、UI 精修。

## 2. 技术决策落位

| 决策点 | 结论 | 依据 |
|---|---|---|
| 审计链开关 | 默认开启，无关闭开关 | ADR-0015；可关闭的审计无意义 |
| 链算法 | SHA-256 over RFC 8785 JCS；`prev_hash` + 记录规范字段 | 复用 `internal/jcs`，避免编码歧义 |
| 追加并发 | 单进程互斥 + SQLite 事务；链头 `audit_chain` 同事务更新 | 单实例锁 + 单写者 |
| 防改删 | `BEFORE UPDATE/DELETE` 触发器 `RAISE(ABORT)`；插入校验前链头 | 阻止误改与 naive 插入 |
| 遗留行 | Phase 4 前无 hash 的行只允许出现在首个密封行之前；之后出现空 hash 视为破损 | 不伪造"历史已密封" |
| 威胁模型 | 防误改/局部篡改；不防拥有完整 DB 写权限的攻击者重写整链；无外部锚定 | ADR-0015 |
| 写后校验 | 重读目标文件 SHA-256；失败自动回滚（覆盖）或移入备份区（新建） | ADR-0016 |
| 备份版本 | 每文件保留 20 版；全局软上限 512 MiB；超限拒绝新备份 | ADR-0016 |
| 恢复语义 | 恢复前备份当前状态；恢复可再撤销；仅非运行态会话可恢复 | ADR-0016 |
| 新建撤销 | 当前文件 hash 等于创建时 hash 才允许；移入 `discarded/`，不 unlink | ADR-0016 |
| 发布触发 | annotated tag `v*`；tag 必须等于 `build/config.yml` 版本 | ADR-0017 |
| 签名凭据 | 仅 GitHub Actions Secrets；dry-run 用临时测试凭据 | ADR-0017 |
| 更新器 | 本阶段不做；不静默自动更新 | architecture/04 |

## 3. 代码切片与依赖方向

```
internal/audit/
  record.go       Record、规范字段、Hash
  verify.go       流式验证、VerifyResult、Export（JSONL + manifest）
  kinds.go        action/decision 常量（tool_call/approval/backup/...）
internal/rollback/
  writer.go       覆盖/新建写入、版本、写后校验、自动回滚
  restore.go      恢复、撤销创建、路径复核
  store.go        RecordStore 接口、Limits、裁剪与容量
internal/storage/
  audit.go        链式 AppendAudit、ListAudit、VerifyAudit
  backups.go      file_backups CRUD 与 usage
  sqlite.go       迁移后装配、触发器
internal/agent/
  audit.go        审计 helper（trace/turn、fail-closed 放行）
  exec.go         判定/审批/规则/备份审计接线
  manager.go      rollback.Writer 构造与桌面 API 入口
internal/desktop/
  service.go      审计/备份服务方法
frontend/src/components/
  AuditPanel.tsx  最近事件、链状态、验证、导出
  BackupsPanel.tsx 文件/版本、恢复、撤销
migrations/0004_phase4.sql
.github/workflows/release.yml
```

依赖方向：`agent → {audit, rollback, storage, permission, hook}`；`rollback → workspace`（路径复核），只通过接口使用存储；`tool → rollback`（写入委托），`rollback` 不 import `tool`/`agent`；`audit` 是叶子包（可 import `jcs`），不 import `storage`/`agent`。Wails 类型仍收敛在 `internal/desktop`。

## 4. 审计链

### 4.1 数据模型（迁移 0004）

```sql
ALTER TABLE audit_log ADD COLUMN turn_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE audit_log ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN detail_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE audit_log ADD COLUMN prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN entry_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_audit_session ON audit_log (session_id, id);
CREATE INDEX idx_audit_trace ON audit_log (trace_id);

CREATE TABLE audit_chain (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    last_id    INTEGER NOT NULL DEFAULT 0,
    last_hash  TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);
INSERT INTO audit_chain (id) VALUES (1);

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;

CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;

CREATE TRIGGER audit_log_chain_insert BEFORE INSERT ON audit_log
WHEN (SELECT last_hash FROM audit_chain WHERE id = 1) <> ''
 AND (NEW.entry_hash = '' OR NEW.prev_hash <> (SELECT last_hash FROM audit_chain WHERE id = 1))
BEGIN SELECT RAISE(ABORT, 'audit chain mismatch'); END;
```

- `detail_json` 是规范 JSON 对象，只允许短引用/hash/计数；不含正文、参数、提示词、密钥。
- `audit_chain` 只用于追加与快速链头；验证以 `audit_log` 行为准，不信任链头。
- 触发器不阻止合法 INSERT；链头为空的首次旧库升级可插入第一条密封行。

### 4.2 记录与哈希

```go
type Record struct {
    ID        int64  // 数据库分配，不参与 hash
    SessionID string
    TurnID    int64
    TraceID   string
    Action    string // tool_call | approval | permission_rule | hook | backup | audit_verify | recovery
    Decision  string // auto | allow | ask | deny | create | restore | ...
    Reason    string // 短原因，可读，无敏感内容
    Detail    string // 规范 JSON 对象
    CreatedAt string
    PrevHash  string
    EntryHash string
}

func Hash(prev string, r Record) (string, error)
```

`Hash` 用 `jcs.Hash` 对以下对象做 RFC 8785 规范化后 SHA-256（小写 hex）：

```json
{"v":1,"prev":"<prev_hash>","sessionId":"...","turnId":3,"traceId":"...",
 "action":"tool_call","decision":"allow","reason":"...","detail":{...},"createdAt":"..."}
```

- `detail` 先用 `jcs.Canonicalize` 校验并规范化；非法 JSON 拒绝写入。
- `createdAt` 使用 `storage.Now()`（UTC RFC3339Nano），先定值再算 hash。
- 不在 hash 内放数据库自增 `id`：顺序由 `id` + `prev_hash` 共同保证；触发器禁止改 id。
- 域分隔由 `"v":1` 承担；未来算法升级写新 ADR。

### 4.3 追加

`AppendAudit(ctx, Record)`：

1. `auditMu` 互斥（单进程；单实例锁保证无第二个写者）。
2. 规范化 `Detail`；补 `CreatedAt`。
3. 开启事务，读 `audit_chain(last_id,last_hash)`。
4. `PrevHash = last_hash`；计算 `EntryHash`。
5. `INSERT INTO audit_log(...)`；`UPDATE audit_chain SET last_id=last_insert_rowid(), last_hash=?, updated_at=?`。
6. 提交。任一步失败回滚，不留下半条链。

**fail-closed 规则**：任何"放行执行"（auto allow、审批 allow、持久规则命中）在动作前写审计；写失败则拒绝执行并上屏错误。拒绝类事件写失败只告警（动作已被阻断）。Hook 内部审计仍为尽力而为（接口不返回错误），其拒绝/改参结果由 agent 的 tool_call 审计兜底。

### 4.4 验证

`Verify(ctx)` 流式按 `id` 读取：

1. 找到第一条 `entry_hash <> ''` 的行，之前的行计为 `LegacyRows`（未密封，明示）。
2. 密封段：期望 `prev_hash` = 上一条密封行 `entry_hash`（第一条为 `''`）；不等 → `BrokenID`。
3. 重算 `Hash(prev, row)`，与 `entry_hash` 不等 → `BrokenID`。
4. 密封段出现 `entry_hash=''` → `BrokenID`（非法插入）。
5. 结束后比对 `audit_chain.last_id/last_hash` 与最后一条密封行；不一致 → `HeadMatch=false`（算破损）。
6. 返回 `{OK, SealedRows, LegacyRows, FirstSealedID, LastID, HeadHash, BrokenID, BrokenReason, HeadMatch}`。

空库/仅遗留行时 `OK=true` 但 `LegacyRows>0`，UI 明示"历史未密封"，不得宣称全量可信。

### 4.5 覆盖清单

| Action | Decision | Detail 关键字段 | 时机 |
|---|---|---|---|
| `tool_call` | `auto` / `allow` / `ask` / `deny` | toolRef, matchKey, execHash, trust, scope, level, quota | 每次工具判定 |
| `approval` | `allow` / `deny` | approvalID, scope, toolRef, execHash, contentTrust | 审批落库后 |
| `permission_rule` | `create` / `hit` / `delete` | ruleID, toolRef, matchKey | 规则变更/命中 |
| `hook` | `warn` / `deny` / `fail_closed` / `patch` | hook, point, fieldCount | Hook 结果 |
| `backup` | `create` / `restore` / `undo_create` / `prune` / `quota` / `verify_fail` | backupID, relPath, version, sha256, size, rollback | 备份/恢复/校验 |
| `audit_verify` | `ok` / `broken` | sealed, legacy, brokenID, headHash | 用户验证 |
| `recovery` | `mark_interrupted` / `mark_unknown` | count | 启动恢复 |

- 工具判定与审批是两条独立记录：判定回答"引擎怎么判"，审批回答"谁批准"。
- `relPath` 仅审计备份相关事件；不记录工作区绝对路径、文件内容或工具参数。
- 现有 Phase 3 的 `AppendAudit(sessionID, action, decision, reason)` 调用全部迁移到 `audit.Record`；旧行保留原字段语义。

### 4.6 导出

`Export(ctx, dir)`：

1. 先 `Verify`，再按 `id` 写 `audit-<UTC 时间戳>.jsonl`（每行一条完整记录，含 hash）。
2. 同目录写 `<同名>.manifest.json`：`version/exportedAt/sealedRows/legacyRows/headHash/verify`。
3. 目录 `0700`、文件 `0600`；临时文件 + fsync + 原子 rename。
4. 返回两个路径与验证结果；链破损时仍导出并标 `ok=false`，保留证据。

导出是可复核的**证据副本**，不是重新密封；manifest 本身不签名（本地文件）。

### 4.7 威胁模型（必须写进 UI/文档）

- 能检测：单行改写、删除中间行、插队、链头不一致、合法追加后的空 hash 插入。
- 不能检测：拥有数据库完整写权限的攻击者重算整条链；删库/全量截断；替换 `audit_chain` 后同步重写。
- 结论：这是**本地完整性护栏**，不是对抗性证明；外部锚定/WORM 留待有合规需求时立 ADR。

## 5. 备份与回滚

### 5.1 数据模型

```sql
CREATE TABLE file_backups (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions(id),
    turn_id        INTEGER NOT NULL DEFAULT 0,
    rel_path       TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('overwrite', 'create')),
    reason         TEXT NOT NULL DEFAULT 'write', -- write | pre_restore | restore_create
    version        INTEGER NOT NULL,
    mode           INTEGER NOT NULL DEFAULT 420,  -- 0644
    size           INTEGER NOT NULL DEFAULT 0,
    backup_rel     TEXT NOT NULL DEFAULT '',      -- 相对备份根
    backup_sha256  TEXT NOT NULL DEFAULT '',
    written_sha256 TEXT NOT NULL DEFAULT '',
    discarded_rel  TEXT NOT NULL DEFAULT '',      -- 撤销创建后的移动目标
    restore_of     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    verified_at    TEXT NOT NULL DEFAULT '',
    restored_at    TEXT NOT NULL DEFAULT '',
    UNIQUE (session_id, rel_path, version)
);
CREATE INDEX idx_file_backups_session ON file_backups (session_id, created_at);
```

- `backup_rel`/`discarded_rel` 存相对备份根路径，DB 不绑死绝对路径。
- `version` 按 `(session_id, rel_path)` 从 1 递增；会话内写操作串行，恢复/撤销只在非运行态。
- `size` 为文件字节数；`mode` 为恢复时使用的权限位。

### 5.2 目录布局

```
<dataDir>/backups/<sessionID>/<relPath>/v000001-<backupID>.bak
<dataDir>/backups/<sessionID>/discarded/<relPath>/<backupID>.trash
```

- 备份文件统一 `0600`；目录 `0700`；原权限位只存 DB，恢复时 `Chmod`。
- 路径由 `filepath.Join` + `filepath.Rel` 校验，禁止绝对路径、`..`、NUL；备份根外一律拒绝。
- 版本文件名为固定宽度 + 随机 ID，避免中文/空格/Windows 保留名问题。

### 5.3 写入管道

`tool.WriteFile` 保留参数解析、工作区解析、敏感路径硬拒、父目录存在检查；随后的文件写入委托 `rollback.Writer.Write`：

1. **覆盖**：检查备份区软上限 → 复制原文件到临时文件 → fsync → chmod 0600 → rename 为版本文件 → 计算 `backup_sha256` → 插入 `overwrite` 记录（`reason=write`）。任一步失败，目标文件不动。
2. **新建**：先计算内容 SHA-256，插入 `create` 记录（版本号、mode、size、`written_sha256`）；插入失败则拒绝创建。写前落 hash 可让崩溃后的"撤销创建"对缺失文件保持幂等。
3. 目标目录写临时文件 → fsync → chmod 原权限（新建 0644）→ 原子 rename；POSIX 下尽力 fsync 父目录。
4. 重读目标文件计算 SHA-256，与内存内容 hash 比对；更新 `verified_at`；覆盖写的 `written_sha256` 在成功后更新。
5. **校验失败**：覆盖 → 用刚创建的版本原子恢复目标；新建 → 移动到 `discarded/`。写入审计 `backup/verify_fail`，返回 `rollback.VerifyError`；工具状态为 `failed`，不得声称成功。
6. 按保留策略裁剪该文件旧版本；记录 `backup/prune` 审计。
7. 返回 `WriteOutcome`：created、version、backupID、sha256、size、verified、rolledBack。

`WriteResult` 扩展同名字段；agent 在工具结果落库后写 `backup/create` 审计（含 turn/trace）。

### 5.4 恢复与撤销

**恢复（overwrite 版本）**：

1. 仅会话非 `thinking/running_tool/persisting/awaiting_approval/paused` 时允许；运行中返回 `ErrBusy`。
2. 加载记录 → 校验 `backup_rel` 在备份根内 → 校验 `backup_sha256`（不一致拒绝并审计）。
3. `workspace.Resolve(rel_path)` + `Rel` 复核 + 敏感路径拒绝 + `Lstat` 拒绝符号链接/junction；父目录必须存在，目标不得是目录。
4. 目标存在：创建 `pre_restore` 版本（当前状态），检查软上限；目标不存在：插入 `restore_create` 记录（以 `backup_sha256` 作为 `written_sha256`）。
5. 备份文件复制到目标目录临时文件 → fsync → chmod `mode` → 原子 rename。
6. 重读校验 `backup_sha256`；失败则恢复安全副本（目标不存在则移入 `discarded/`），审计 `verify_fail`。
7. 标记原记录 `restored_at`；安全副本 `restore_of=原记录 ID`；审计 `backup/restore`。

**撤销创建（create 记录）**：

1. 仅非运行态；记录未 `restored_at`；`written_sha256` 非空。
2. 目标必须存在、是普通文件、非链接；当前 SHA-256 必须等于 `written_sha256`；否则拒绝"文件创建后被修改"。
3. 移动到 `discarded/<rel>/<id>.trash`（跨卷时复制 + fsync + 删除），更新 `discarded_rel/restored_at`，审计 `backup/undo_create`。
4. 目标已不存在：标记 `restored_at`，按幂等成功返回并注明"已不存在"。

恢复/撤销本身不执行 shell/删除工具；所有路径仍受工作区与敏感路径约束。

### 5.5 保留与容量

- 每 `(session, rel_path)` 保留最近 **20** 个版本（含 `pre_restore` 安全副本）；新版本插入后删除更旧记录与文件。
- 全局软上限 **512 MiB**：`SUM(size)`（仅 `backup_rel` 或 `discarded_rel` 非空的行）；超限时**拒绝新的覆盖写/安全副本**，返回可读错误并审计 `backup/quota`；新建（不占备份区）不受影响，撤销创建不因上限被拒。
- 并发会话可能短暂越过软上限（按检查时快照）；这是有界风险，不做全局预留。
- 不做自动跨会话清理；提供"清理本会话旧备份"作为后续候选项（本期不做）。

### 5.6 路径与竞态

- 写前/恢复前都重新 `Resolve`，与 Phase 3 TOCTOU 复核一致；执行与 rename 之间仍有残窗，Phase 5 沙箱关闭。
- 恢复/撤销与工具执行互斥：运行态拒绝；同会话工具串行，避免版本号竞态。
- 跨卷移动（工作区与数据目录不同盘）使用复制 + fsync + 删除；失败保留两侧，不吞错。

## 6. Agent、桌面与前端

### 6.1 trace 与审计 helper

- `Session` 增加 `traceID`（每次 run 新生成，`storage.NewID()`），`auditNow(record)` 自动补 session/turn/trace。
- `execOne` 在判定后写 `tool_call` 审计（放行前 fail-closed）；`askApproval` 写 `approval`；规则命中/创建/删除写 `permission_rule`；`WriteResult` 落库后写 `backup`；启动恢复在 `main` 写 `recovery`。
- Hook 回调在 `main` 映射为 `audit.Record{Action:"hook"}`，保留现有日志输出。

### 6.2 桌面 API

```go
ListAudit(sessionID string, limit int) (AuditPage, error)
VerifyAudit() (AuditVerify, error)
ExportAudit() (AuditExport, error)
ListBackups(sessionID string) ([]BackupGroup, error)
RestoreBackup(sessionID, backupID string) (BackupRestore, error)
UndoCreatedFile(sessionID, backupID string) (BackupRestore, error)
```

- `AuditPage` 含最近 N 条（默认 100，上限 500）与当前验证结果；`AuditVerify` 见 §4.4；`AuditExport` 含 jsonl/manifest 路径与结果。
- `BackupGroup` 按 `rel_path` 聚合版本；`BackupRestore` 含 restored/created、sha256、是否自动回滚、提示。
- 恢复/撤销审计由 `agent.Manager` 写，trace 新生成；错误返回中文可读文本。

### 6.3 前端

- 顶栏"审计"按钮打开 `AuditPanel`：链状态徽章（完整/破损/含遗留）、最近事件表（时间/动作/决策/原因）、"验证"、"导出"；破损时红色横幅显示首个坏行。
- 会话区"备份"按钮打开 `BackupsPanel`：按文件分组，显示版本、大小、时间、是否可撤销创建；恢复需二次确认（说明"恢复前会备份当前内容"）；运行中禁用并说明原因。
- 所有新增文本走 `SafeText`；不渲染 HTML；不新增外链；CSP 不变。
- 文件体积控制：新面板独立组件；App.tsx 只持有开关与数据，必要时抽 `panels.ts` 以守 600 行。

## 7. 迁移与兼容

- 迁移 0004 在 0003 之后执行；空库与旧库均可幂等升级。
- 旧 `audit_log` 行 `prev_hash/entry_hash=''`，验证器计为 `LegacyRows`；首个新行 `prev_hash=''` 开始密封段。
- 旧 `tool_calls.status`、审批、规则表不变；`WriteResult` 新字段对旧 transcript 只是多字段，前端按可选处理。
- 迁移测试：构造 0003 数据库（含旧审计行/工具调用）→ 打开 → 校验版本、字段、触发器、遗留行计数。

## 8. 发布签名（摘要，详见 release-signing.md）

- `.github/workflows/release.yml`：`push.tags: v*` + `workflow_dispatch(dry_run)`。
- 版本一致性：tag 去掉 `v` 必须等于 `build/config.yml` 的 `info.version`，否则失败。
- Windows：构建 → Authenticode 签名 exe → NSIS → 签名安装包 → `signtool verify /pa /all`。
- macOS：构建 `.app` → Developer ID 签名 → 公证 → staple → `codesign --verify --deep --strict` + `spctl --assess` + `stapler validate`。
- Linux：构建 `.deb/.rpm/AppImage` → GPG 签包 → 生成 `SHA256SUMS` → GPG 签 checksum → `gpg --verify`。
- 凭据只从 GitHub Actions Secrets 注入；dry-run 用临时自签证书（Windows）、ad-hoc（macOS）、临时 GPG 密钥（Linux），只验证流水线，不创建 Release。
- 没有真实凭据时正式发布任务**失败**；不得降级为未签名发布。

## 9. 测试策略

| 层 | 用例 |
|---|---|
| 审计哈希 | 固定向量、字段变更、`detail` 非规范 JSON 拒绝、JCS 一致性 |
| 审计链 | 改单行/删中间行/插队/空 hash 插入/链头不一致均定位；遗留段；空库；重启续链 |
| 触发器 | UPDATE/DELETE 被拒；合法 INSERT 通过；错误 prev 被拒 |
| 导出 | JSONL 行数/hash 可复算；manifest 与验证结果一致；权限 0600 |
| 备份 | 多版本、保留 20、软上限拒绝、备份 hash 校验、跨卷移动 |
| 恢复/撤销 | 恢复成功、恢复前安全副本、再恢复、撤销创建、修改后拒绝、缺失幂等、运行态拒绝 |
| 写后校验 | 故障注入导致 hash 不一致 → 自动回滚/移入备份区 + 审计；正常路径 `verified_at` |
| Agent | 判定/审批/规则/备份审计齐备；审计写失败时放行被拒；trace/turn 正确 |
| 迁移 | 0003→0004 旧库、幂等、旧行遗留计数 |
| 前端 | 面板 SSR 文本、破损横幅、恢复禁用、SafeText；渲染安全门禁 |
| 发布 | dry-run workflow 三平台签名/验签；缺凭据失败；版本不一致失败 |

## 10. 风险与遗留

- 审计 fail-closed 可能在 DB 故障时阻断放行；错误必须可读并提示备份/修复，不静默继续。
- 触发器与链不防全权限攻击者；外部锚定未做；文档与 UI 必须如实表述。
- 备份软上限是快照检查，不是硬配额；跨卷移动与恢复有额外磁盘峰值。
- 恢复/撤销与执行之间仍有路径残窗；Phase 5 沙箱前不宣称完全消除。
- 外部进程若在"备份→写入→重读"窗口内改动同一文件，可能被判定为写后校验失败并触发回滚；桌面单用户场景接受该毫秒级风险，后续可评估文件锁或沙箱。
- 真实签名凭据未定；A7 条件验收，dry-run 不能替代平台信任链验证。
- 审计导出含文件名与 session ID；导出目录 0700/0600，用户自行保管。
- 备份元数据（`file_backups`）丢失或被删时，磁盘上的版本文件会成为孤儿；本阶段不做备份目录扫描重建，需人工按路径找回。
- 不引入新 Go 运行时依赖；签名使用平台工具与 Wails 现有任务。
