# H-007 Phase 4 实现完成（待 A5 真机与 A6 dry-run）

## 交接目标

Phase 4（不可变审计 + 备份式回滚 + 三平台签名 CI）实现完成、本地门禁全绿；下一个会话要把 A5/A6/A8 闭合并按流程合并打 `phase-4.end`。分支 `feat/phase4-audit-rollback-signing`，所有者已签收推送（2026-10-31）。**`v*` 发版另排**：UI 细设计打磨未完成，首发平台 Windows 优先，macOS/Linux 只验收"可构建 + 可签名 + 可验签"。

## 已完成

- 迁移 0004：审计 `turn_id/trace_id/detail_json/prev_hash/entry_hash` + `audit_chain` + UPDATE/DELETE/INSERT 触发器 + `file_backups`。
- `internal/audit`：JCS+SHA-256 链、流式验证（破损/遗留/链头）、JSONL+manifest 导出。
- `internal/rollback`：版本化备份、20 版/512 MiB 软上限、恢复前安全副本、撤销创建入 `discarded/`、写后复核自动回滚。
- 接线：`audit.Record` 统一入口、trace/turn 贯穿、放行 fail-closed、`write_file` 委托 Writer、Hook/恢复写审计。
- 桌面 6 个新方法 + 前端"审计 / 备份"抽屉（`AuditPanel`/`BackupsPanel`/`SecurityPanel`）。
- `.github/workflows/release.yml`（prepare/windows/macos/linux/release）+ `scripts/check-version.sh`（标签+配置+生成资产四重校验）。
- 报告草稿 `docs/design/phase-4/report.md`；架构 02/04 已回填；CHANGELOG 0.1.014。

## 未完成 / 下一步

1. A5 真机：所有者在隔离 `APPDATA` 验面板（链状态、导出、恢复二次确认、撤销创建、运行中拒绝）。
2. 已提交并推送 → 等 CI 绿 → 手动触发 `Release`（`workflow_dispatch`，一律 dry-run、不发布）拿 A6 证据；首跑大概率要按日志修 NSIS 安装、AppImage 文件名、`dpkg-sig`/`rpmsign` 行为。
3. A7 需真实凭据（Windows PFX、Apple Developer ID + 公证、Linux GPG 私钥/Key ID）→ GitHub Secrets；按所有者决定随首发排期主动延后，没有凭据就记"条件未执行"，不得发布未签名产物。
4. A8 闭合后报告转定稿、看板勾 A5~A8、合并 main、tag `phase-4.end`（阶段结束 tag，不是 `v*` 发布标签）。

## 本轮踩到的坑（已入库）

- P-011 Wails 构建资产停留在 init 模板值且版本落后 → 重生 + 版本门禁扩展到资产。
- P-012 `wails3 tool sign --pgp-key` 是密钥 ID 不是文件；`rpm --checksig` 要导入公钥并断言 `digests signatures OK`；产物名是小写 `happywork`，Wails 的 `windows:sign:installer` 任务路径与 `project.nsi` 不一致。
- P-013 审计证据写入工作区绝对路径，本机（Windows）测不出、Linux CI 才红：匹配键被小写化 + JSON 双层转义，字节精确的 `Contains` 断言失效。已改为源头相对化 + 归一化比较。
- 另修：`internal/agent/exec.go` 超 600 行 → 拆 `approval.go`；测试脚手架 `fakeProvider.calls` 无锁读 → `-race` 全绿。
- P-014 SQLite pragma 按连接生效而池子无上限 + "查最大值再插入"跨调用 → 并发下 `database is locked (517/5)` 与 `UNIQUE(session_id, seq)`；修法是 `SetMaxOpenConns(1)` + 序号/版本在事务内分配（`internal/storage/sqlite_write.go`），并发回归测试 `sqlite_contention_test.go`。
- 远端 CI 首跑 `41094526014`：`build (windows/macos/ubuntu)` 三 job 通过，`check` 因 P-013 失败；复跑 `41096410911` 三 job 仍通过，`check` 因 P-014 失败。两处修复已推，A8 需要第三次 CI 绿作为证据。

## 快速恢复上下文

先读本卡 + `.agents/CONTEXT.md`，再看 `docs/roadmap/phase-4-audit-rollback-signing.md` §5 与 `docs/design/phase-4/report.md` §2/§5（待办清单）。
