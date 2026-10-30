# P-014 SQLite 连接池让 WAL/busy_timeout 只作用于一条连接，并把读改写竞态暴露成 BUSY

## 现象

分支推送后 CI 的 `check` job 两次失败，本机全绿：

```
test agent: persist audit: storage: insert audit: database is locked (517)
--- FAIL: TestPhase4UndoCreatedFileMovesToBackupArea
    phase4_backups_test.go:197: UndoCreatedFile: rollback: mark undo: database is locked (5) (SQLITE_BUSY)
```

另外本机跑并发回归测试还看到第二层症状：`UNIQUE constraint failed: messages.session_id, messages.seq`、`UNIQUE constraint failed: file_backups.session_id, rel_path, version`——不是锁，是**丢了更新**。

## 根因

1. **pragma 是"每条连接"生效的，不是"每个库"生效的**。`Open()` 用 `db.Exec("PRAGMA busy_timeout = 5000")` / `journal_mode = WAL` / `foreign_keys = ON`，而 `sql.DB` 是连接池且没设上限：这三条只落在"当时借到的那一条连接"上，之后新建的连接全是默认值（`busy_timeout=0`、`foreign_keys=OFF`）。
2. **读→写升级型事务在多连接下必然撞 BUSY**。`AppendAudit` 开的是延迟事务：先 `SELECT` 链头再 `INSERT/UPDATE`。另一条连接上已有写者提交过时，SQLite 直接返回 `SQLITE_BUSY_SNAPSHOT`(517)，**这是升级死锁，busy_timeout 不会重试**；普通写语句也因为没有 pragma 而立刻报 `database is locked (5)`。Phase 4 之前只有 actor 单 goroutine 写库，问题被掩盖；加了桌面面板（`ListAudit`/`VerifyAudit`/`ListFileBackups`）与备份元数据写入后，同一进程里出现了真正的读写并发。
3. **"先查最大值再插入"不是原子的**。`AppendMessage` 用 `NextSeq()`（独立查询）再 `INSERT`；`file_backups.version` 同理。两个调用交错就得到同一个 seq/version → UNIQUE 冲突。单连接只是让它们排队，**不自动把两次调用合并成一个事务**。

## 修复

- `Open()` 里 `db.SetMaxOpenConns(1)` + `SetMaxIdleConns(1)`：一条连接既是 pragma 的作用域，也消除了跨连接的升级死锁。桌面单机场景下，串行访问比 WAL 的读写并发更值钱（注释写明了这个取舍）。
- 新增 `internal/storage/sqlite_write.go`：`writeTx` helper + 事务版 `nextSeq`/`nextBackupVersion`；`AppendMessage` 与 `CreateFileBackup` 改为"读最大值 + 插入 + touch"在同一事务内完成；`AppendAudit` 本来就是事务，现在在无升级死锁的连接上稳定工作。
- `internal/storage/sqlite_contention_test.go`：4 写者 × 4 读者 × 25 轮的并发回归，断言无 `database is locked`、无 UNIQUE 冲突、事后整链可验证且行数正确。**修前本机三条症状全部复现，修后 `-count=3` 稳定绿。**

## 预防

- 用 SQLite 时把 pragma 当成"连接的属性"：要么 `SetMaxOpenConns(1)`，要么把 pragma 写进 DSN 参数，别假设 `db.Exec(PRAGMA)` 对整个池生效。
- 出现 `SQLITE_BUSY_SNAPSHOT`(517) 就是升级死锁的信号，加重试/加超时都没用，只有改成 `BEGIN IMMEDIATE` 或串行化连接才有效。
- 任何"查最大值再插"的序号/版本分配必须在同一事务里；单连接不等于事务。
- 新增"面板轮询 + 后台写入"这类并发面时，配套一条并发测试；本项目已有的单线程测试全绿不代表 store 层没有竞态（CI 是这里的唯一发现者）。

##（如复发）再次发生记录

- （无）

## 归属

Phase 4 审计/备份接线（2026-10-31）/ `internal/storage/sqlite.go`、`internal/storage/sqlite_write.go`、`internal/storage/sqlite_contention_test.go`；对应 CI run `41094526014`、`41096410911`。与 P-013 同一轮：那条是"本地测不出、CI 才发现"，这条也是。
