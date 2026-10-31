# 工作上下文交接卡

- 更新：2026-10-31（会话主题：Phase 4 实现完成 + 门禁机制改造 ADR-0018）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1 ✅ 完成（tag `phase-1.end`）**
- **Phase 2 ✅ 完成（tag `phase-2.end`）**：8/8 验收，报告定稿，main CI 绿。
- **Phase 3 ✅ 完成（tag `phase-3.end`）**：A1~A8 通过；CI run 41066195833 全绿；报告 [docs/design/phase-3/report.md](../docs/design/phase-3/report.md)。
- **Phase 4 实现完成并已合并 main**（merge `94d576d`；分支期 `ci.yml` 三轮：两轮红在 P-013/P-014，第三轮 `41098214658` 全绿，main 上 `41102545807` 全绿）。A1~A4 通过；A5 真机待验；A6 已按 ADR-0018 收敛为"Windows 单平台 dry-run"，macOS dry-run 已在 run `41102612818` 绿过一次，Windows 待复跑（签名链路已本机跑通，只差安装包那一环）；A7 随首发延后。报告（草稿）见 [docs/design/phase-4/report.md](../docs/design/phase-4/report.md)。
- **门禁机制已换（ADR-0018）**：日常验收 = 本机 `bash scripts/gate.sh --race`（本地与 CI 同一份脚本）；`ci.yml` 取消 push 触发，改手动/PR；`release.yml` dry-run 默认只跑 windows；全部 job 加 `timeout-minutes` + `concurrency`。
- 分支：Phase 4 实现与门禁机制改造都已在 main；A6 的 dry-run 取证直接在 main 上手动触发（`workflow_dispatch` 只认默认分支，P-015）。
- 所有者决定（2026-10-31）：**暂不打 `v*` 标签、不发版**——UI 细设计打磨未完成；首发平台 **Windows 优先**，macOS/Linux 只保证"可构建 + 可签名 + 可验签"，不保证首发打磨度；**Actions 分钟是有额度的资源，不要每轮推送都跑远端 CI**。

## 上一轮完成了什么（3~5 条）

- 审计链：JCS + SHA-256 逐行链接、`audit_chain` 同事务更新、UPDATE/DELETE 触发器、插入校验前驱；验证器区分完整/破损/遗留段并定位首个坏行；JSONL + manifest 导出（0700/0600）。
- 版本化备份与回滚：每文件 20 版、512 MiB 软上限拒绝、恢复前安全副本、撤销创建移入 `discarded/`（不 unlink）、写后 SHA-256 复核失败自动回滚；放行路径审计写失败即拒绝执行。
- 接线与 API：`audit.Record` 统一入口 + trace/turn 贯穿；`write_file` 委托 `rollback.Writer`；桌面新增 6 个方法；前端"审计 / 备份"抽屉（纯文本渲染，CSP 不变）。
- 发布流水线：`v*` 标签三平台签名/验签 + `release` job 汇总校验和；`workflow_dispatch` 一律 dry-run；缺凭据的正式标签失败。
- 顺带修复：Wails 构建资产仍是 init 模板值/旧版本（已重生入库并扩展版本门禁）、`exec.go` 超 600 行（拆出 `approval.go`）、测试脚手架 `fakeProvider.calls` 数据竞争（`-race` 现全绿）。
- 推送后 CI 首跑（run `41094526014`）：三平台构建 job 全绿，`check` 抓到审计证据含工作区绝对路径（只在 Linux 复现）——源头改为工作区相对 + 测试归一化比较，先红后绿（P-013）。
- CI 复跑（run `41096410911`）仍红在 `check`：`database is locked (517/5)`。根因是 SQLite pragma 按连接生效而池子无上限，加上"消息序号 / 备份版本号"跨调用取最大值。已把连接池固定为 1、序号与版本分配并入插入事务，新增 4 写者 × 4 读者的并发回归测试（P-014）。
- 按所有者要求**改门禁机制**（ADR-0018）：新增本地同源脚本 `scripts/gate.sh`（本地与 CI 跑同一份），`ci.yml` 去掉 push 触发、矩阵首发默认只 `windows`，`release.yml` dry-run 增加 `platforms` 选择并默认 `windows`，全部 job 补 `timeout-minutes` + `concurrency`；Windows dry-run 不再导入受信任根，改用内存临时证书 + `Get-AuthenticodeSignature` 断言。
- 把 Windows 签名链路搬回本机并修到真能证明问题：`scripts/windows-sign-check.ps1` 只签 `bin\happywork.exe` 的副本，验签断言签名者指纹 + EKU，负向对照改为主体篡改且必须报 `TRUST_E_BAD_DIGEST (0x80096010)`（尾部篡改只破坏证书表，P-017）；顺带发现 PS 5.1 按行读 UTF-8 必须 `-Encoding UTF8`。
- 又修掉一处真实缺陷：暂停路径先发状态后发事件，导致"会话暂停但看不到原因"的窗口；改为先发事件再翻状态，并在测试里固化事件顺序不变量（本机并发下先红后绿）。`.gitattributes` 补齐 go/ts/tsx/css/json/yml/md 等 `eol=lf`，消掉 Windows 上的 prettier/gofmt 误报与幻影 diff。

## 下一步（按优先级，注明阻塞项）

1. **A5 真机验收（所有者）**：按 [docs/design/phase-4/a5-checklist.md](../docs/design/phase-4/a5-checklist.md) 在隔离 `APPDATA` 下走一遍——写文件审批 → 审计面板链状态/验证/导出（导出里不得出现工作区绝对路径与正文）→ 备份面板恢复（二次提示 + 安全副本）→ 撤销创建 → 运行中恢复被拒。
2. **A6 Windows dry-run 复跑**（唯一剩下的取证动作）：把 `exit 0` 修复合并 main 后 `gh workflow run release.yml`（默认只跑 windows）。run `41156332487` 已确认单平台门控与 5 分钟快速失败；run `41156918894` 的日志已覆盖构建、签名、安装包、验签与篡改对照（全部通过），只差一次绿 run 收口——它红在 runner 的 `pwsh -command` 把负向对照的 `signtool verify` 退出码当成了步骤退出码（P-019，已修）。下一次要看到：`makensis` 找到并产出安装包、`signtool sign` 签 exe 与安装包、`Get-AuthenticodeSignature` 断言（签名者指纹 + EKU）、主体篡改副本报 `0x80096010`、产物上传，且整 run 绿。
3. **A8 收尾**：Windows dry-run 绿 + A5 签收 → 报告定稿 → 看板勾 A5/A6/A8 → tag `phase-4.end`。**不打 `v*`**（发版另排）。
4. **A7 条件项（随首发排期主动延后）**：所有者提供 Windows PFX、Apple Developer ID/公证、Linux GPG 私钥与 Key ID 后配成 GitHub Secrets，补跑正式签名并回填证书主体/指纹证据。
5. 可选真机补录（Phase 3 遗留）：持久短语与 secret-scan 的 GUI 提交（宿主安全分类器限制，集成测试已覆盖）。
6. UI 细设计与打磨（含审计/备份面板灰盒样式）按所有者决定排在发版之前，Phase 4 不强行收尾。

## 未决问题

- **"每轮推送都有远端 CI"这一保证已取消**（ADR-0018 的代价）：远端只在里程碑/手动触发时跑。合并 main 前的绿=本机 `gate.sh --race` 的绿；若哪天要恢复严格性，改 `ci.yml` 的 `on:` 即可，但要先想清楚额度。
- **Linux 包签名当前不可用**：`ubuntu-latest`（Ubuntu 26.04）镜像里没有 `dpkg-sig`，`linux` job 一跑就红。按首发范围它现在不会被触发；真要支持 Linux 时需重做 deb/rpm 签名方式（分离签名或钉镜像版本）。
- 本机**有** Windows SDK 的 `signtool`（签名/验签已用 `scripts/windows-sign-check.ps1` 在本地跑通），但**没有 `makensis`**：安装包那一步只能在远端验证，所以 A6 的 Windows 部分仍需至少一次远端绿。
- **`wails3 tool sign` 在 Windows 上是空实现**（本机实测报 `built-in Windows signing not yet implemented`）；流水线已改为直接调 `signtool sign`。help 里列出的命令不等于实现了（P-012 复发记录）。
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
- **门禁在本地跑**：日常提交前 `bash scripts/gate.sh --race`（本地与 CI 同源，ADR-0018）。`ci.yml` 已无 push 触发，别指望"推送会自动有人验证"；合并 main 前必须亲自确认本地全绿。
- **Actions 分钟是配额资源**（Linux 1× / Windows 2× / macOS 10×）：新 job 必须有 `timeout-minutes`，workflow 必须有 `concurrency` + `cancel-in-progress`；dry-run 默认只跑首发平台 windows，macOS/Linux 显式勾选。
- **CI 里绝不导入受信任根存储**：无人值守会弹确认框、静默挂死 63 分钟（P-016）。dry-run 验签用 `Get-AuthenticodeSignature` 断言签名存在/签名者/EKU（按 OID）；"有没有签名"看 `SignerCertificate`，不看状态词；`signtool /pa` 只留给真实证书路径。
- **负向对照要打中真正的检查点**：证明"篡改会被拒"必须是改镜像主体 + `signtool` 报 `0x80096010`（`TRUST_E_BAD_DIGEST`）；改尾部只破坏 PKCS#7 证书表，得到的 `NotSigned`/链错误不算证据（P-017）。本地签名脚本只签副本，`bin\happywork.exe` 保持未签名供 A5 使用。
- 用 PowerShell 5.1 按行解析 UTF-8 文件（YAML/workflow/脚本）必须 `-Encoding UTF8`：默认 ANSI 会吞换行，实测 644 行读成 594 行；PS 5.1 跑 BOM-less 中文脚本输出乱码，所以本机 `.ps1` 输出保持 ASCII（P-016）。
- 状态事件顺序不变量：**先 `emitEvent` 再 `setState`**——观察方读到 `StatePaused` 时必须已经能看到 `EventPaused`（本轮修过一处，新暂停路径照此办理）。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
