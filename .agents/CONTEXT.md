# 工作上下文交接卡

- 更新：2026-10-31（会话主题：Phase 4 实现完成并推送，等待 A5 真机验收与 A6 远端 dry-run）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1 ✅ 完成（tag `phase-1.end`）**
- **Phase 2 ✅ 完成（tag `phase-2.end`）**：8/8 验收，报告定稿，main CI 绿。
- **Phase 3 ✅ 完成（tag `phase-3.end`）**：A1~A8 通过；CI run 41066195833 全绿；报告 [docs/design/phase-3/report.md](../docs/design/phase-3/report.md)。
- **Phase 4 实现完成（本地门禁全绿，A1~A4 自动化通过；A5/A6/A7/A8 未闭合）**：迁移 0004、`internal/audit`、`internal/rollback`、agent/tool/desktop 接线、审计/备份面板、`release.yml` + `scripts/check-version.sh` 全部落位。报告（草稿）见 [docs/design/phase-4/report.md](../docs/design/phase-4/report.md)。
- 分支：`feat/phase4-audit-rollback-signing`（所有者已签收推送；尚未合并 main，等 A5/A6/A8）；main 常绿。
- 所有者决定（2026-10-31）：**暂不打 `v*` 标签、不发版**——UI 细设计打磨未完成；首发平台 **Windows 优先**，macOS/Linux 只保证"可构建 + 可签名 + 可验签"，不保证首发打磨度。

## 上一轮完成了什么（3~5 条）

- 审计链：JCS + SHA-256 逐行链接、`audit_chain` 同事务更新、UPDATE/DELETE 触发器、插入校验前驱；验证器区分完整/破损/遗留段并定位首个坏行；JSONL + manifest 导出（0700/0600）。
- 版本化备份与回滚：每文件 20 版、512 MiB 软上限拒绝、恢复前安全副本、撤销创建移入 `discarded/`（不 unlink）、写后 SHA-256 复核失败自动回滚；放行路径审计写失败即拒绝执行。
- 接线与 API：`audit.Record` 统一入口 + trace/turn 贯穿；`write_file` 委托 `rollback.Writer`；桌面新增 6 个方法；前端"审计 / 备份"抽屉（纯文本渲染，CSP 不变）。
- 发布流水线：`v*` 标签三平台签名/验签 + `release` job 汇总校验和；`workflow_dispatch` 一律 dry-run；缺凭据的正式标签失败。
- 顺带修复：Wails 构建资产仍是 init 模板值/旧版本（已重生入库并扩展版本门禁）、`exec.go` 超 600 行（拆出 `approval.go`）、测试脚手架 `fakeProvider.calls` 数据竞争（`-race` 现全绿）。
- 推送后 CI 首跑（run `41094526014`）：三平台构建 job 全绿，`check` 抓到审计证据含工作区绝对路径（只在 Linux 复现）——源头改为工作区相对 + 测试归一化比较，先红后绿（P-013）。
- CI 复跑（run `41096410911`）仍红在 `check`：`database is locked (517/5)`。根因是 SQLite pragma 按连接生效而池子无上限，加上"消息序号 / 备份版本号"跨调用取最大值。已把连接池固定为 1、序号与版本分配并入插入事务，新增 4 写者 × 4 读者的并发回归测试（P-014）。

## 下一步（按优先级，注明阻塞项）

1. **A5 真机验收（所有者）**：隔离 `APPDATA` 下跑 `bin/happywork.exe` —— 写文件审批 → 审计面板链状态/验证/导出 → 备份面板版本列表、恢复（确认二次提示与安全副本）、撤销创建 → 运行中尝试恢复看 `ErrBusy` 提示。
2. **A6 远端 dry-run（分支已推送）**：手动触发 `Release` workflow（`workflow_dispatch`，一律 dry-run、不发布）；按日志修 `choco install nsis`、`dpkg-sig`/`rpm --checksig` 行为、AppImage 文件名等首次实跑问题。三平台全绿才算 A6 通过。
3. **A8 收尾**：远端 CI 第三次复跑确认全绿（前两轮分别红在 P-013 路径泄漏、P-014 存储并发，两处都已修 + 回归测试）→ 报告定稿 → 签收 → 合并 main → tag `phase-4.end`；看板与 CHANGELOG 收口。**不打 `v*` 标签**（发版另排）。
4. **A7 条件项（随首发排期主动延后）**：所有者提供 Windows PFX、Apple Developer ID/公证、Linux GPG 私钥与 Key ID 后配成 GitHub Secrets，补跑正式签名并回填证书主体/指纹证据；届时 macOS/Linux 只验收"可构建 + 可签名 + 可验签"。
5. 可选真机补录（Phase 3 遗留）：持久短语与 secret-scan 的 GUI 提交（宿主安全分类器限制，集成测试已覆盖）。
6. UI 细设计与打磨（含审计/备份面板灰盒样式）按所有者决定排在发版之前，Phase 4 不强行收尾。

## 未决问题

- A6 从未在远端实跑过：workflow 的命令/任务名/产物名都对着本机 Wails v3.0.0 与 `wails3 task --list-all`、`wails3 tool sign -help` 校验过，但 NSIS/AppImage/linuxdeploy 的实际产物命名仍需首跑确认。
- 真实签名凭据是否存在（A7 阻塞项）。
- 审计链威胁模型已写进 UI 与文档：本地完整性护栏，不防全权限重算，无外部锚定。
- 备份元数据丢失会让磁盘版本成为孤儿（本期不做扫描重建）。
- 工具决策徽章仍只在运行期展示（Phase 4 不处理）。

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- 审计放行类事件 fail-closed：写不进审计就不放行；拒绝类只告警。不得伪造"历史已密封"。
- 写进审计的路径/匹配键一律工作区相对（`.` / `src/cmd` / `<outside>`），绝对键只留 `permission_rules` 表；路径不变量的测试要忽略大小写与分隔符，否则又变成"只在 Linux 有效"的检查（P-013）。
- SQLite 纪律：连接池固定 1 条（pragma 按连接生效，多连接会让 busy_timeout/WAL/外键只对第一条有效）；`SQLITE_BUSY_SNAPSHOT`(517) 是读→写升级死锁，加重试无用，必须串行化或用 IMMEDIATE 事务；"查最大值再插入"（消息 seq、备份 version、审计链头）必须在同一事务里，单连接不等于事务（P-014）。
- 备份恢复前先备份当前状态；新建撤销移入 `discarded/`，不直接删除；路径每次重规范化；运行态一律拒绝恢复/撤销。
- 无真实凭据不得发布未签名产物；dry-run 产物只能是 preview，且 `release` job 只取 `*-release` 产物。
- `wails3 tool sign` 的 Linux `--pgp-key` 是**密钥 ID**（内部走 `dpkg-sig -k` / `rpmsign`），不是私钥文件路径；`rpm --checksig` 需先导入公钥，断言要写 `digests signatures OK`。
- 产物名是小写项目名：`bin/happywork.exe`、`bin/happywork-amd64-installer.exe`、`bin/happywork.app`；别按 `productName` 的大写去猜路径。
- 改 `build/config.yml` 版本后必须 `wails3 task common:update:build-assets` 并提交生成物，否则 `scripts/check-version.sh` 会失败（只校验配置不够）。
- 本机 `core.autocrlf=true`：`gofmt -l .` 会把 CRLF 工作树文件全部误报为未格式化；用"去掉 CR 后再 gofmt"的等价的检查（仓库内是 LF，`.gitattributes` 兜底）。
- 600 行硬门槛：`internal/agent/exec.go` 加审计接线时超限，按门槛拆 `approval.go`；新增大文件前先想清楚落位（standards/08）。
- 纯 tool_calls 回合必须落 assistant 消息（P-009）；预设收紧到 once 时关闭持久（P-010）；审批快照只比较一次（P-006）；崩溃恢复只标活动状态（P-008）。
- 敏感文件硬拒不提供审批放行；密钥只走环境变量，不入库/日志/transcript；签名凭据只进 GitHub Secrets。
- 渲染一律纯文本；Hook 改参仅 before_tool 白名单；after 只读；审计不落原文；决策类 CSS 类名走白名单（未知值只落 `other`）。
- 自动化测试/CI 禁外网；只有 manual 标签真机测试可调 DeepSeek；GUI 验收使用隔离 APPDATA。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
