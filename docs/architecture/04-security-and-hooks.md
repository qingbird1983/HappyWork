# 04-安全边界、TOCTOU 管道与扩展

- 状态：定稿（2026-10-30，所有者放行开工）
- 日期：2026-10-30
- 关联：[01-overview](01-overview.md)、[02](02-core-loop-and-data.md)、[03](03-permission-and-approval.md)；[ADR-0003](../adr/ADR-0003-dual-fingerprint.md)、[ADR-0005](../adr/ADR-0005-mvp-security-downgrade.md)；[research/01](../research/01-v2-design-review.md)

## 定位与职责

安全护栏层：在权限判定（03）之上，定义**执行管道的一致性**（TOCTOU）、**犯错的兜底**（备份回滚、沙箱分期）、**扩展的边界**（Hook）与**攻击面最后一公里**（渲染安全）。原则：让 Agent 犯错时的代价足够低，而不是追求完美拦截。

## 三层防线

| 层级 | 手段 | 目的 |
|---|---|---|
| **事前** | 最小权限基线 + 条件策略 + 可逆性标注 | 让 Agent 没机会犯大错 |
| **事中** | Confirm-then-Act + execHash 锁定 + 沙箱隔离（分期落地） | 犯错只伤到隔离区，人看到的 = 执行的 |
| **事后** | 备份式回滚（仅可逆操作）+ 不可变审计 | 错能恢复、可追溯 |

高危操作剥离：改数据库、发邮件、删数据等动作隔离为独立最小权限工具，不与主 Agent 共享权限通道。

## 可逆性标注

> 快照/备份只覆盖本地文件系统。发邮件、推 Git、调外部 API、删远端资源的副作用**不可回滚**，而它们风险最高——与"写本地文件"放进同一框架会制造虚假安全感。

```go
type ToolMeta struct {
    Ref         string      // builtin://write_file
    Action      Action      // read / write / execute / delete / network
    Reversible  bool        // 关键字段
    RetrySafe   bool        // 可逆 ≠ 可重试，两者独立声明
    SideEffects []string    // 声明式副作用描述
}
```

| 可逆性 | 强制策略 |
|---|---|
| `reversible=true` | 可进条件自动放行通道；出错可备份回滚；允许受控重试（配合幂等键） |
| `reversible=false` | 一律强审批（Block 级）；默认 deny 除非显式开启；禁自动重试；不进限额自动放行；执行前必打检查点并明示"此操作不可撤销" |

## TOCTOU 七步管道（含 JCS 规范）

> 核心问题：Hook 在审批后改参、或审批展示原参而执行用改后参数，审批即形同虚设。**管道顺序写死，执行用 hash 锁定。**

1. **模型产出 ToolCall**（call_id / tool_ref / args）。
2. **before_tool_call hooks**：可返回 Patches，仅白名单字段可改；改参本身写审计。
3. **合并 patches → finalArgs**；路径/域名客体做**规范化**（03 §规范化管线）。
4. **计算 `execHash = sha256(JCS(finalArgs))`**。JCS = RFC 8785 JSON Canonicalization Scheme（键序、浮点、Unicode 转义统一）——不定义规范的 canonical JSON 会因编码器差异产生假 `ErrArgsDrift`（v2 缺陷，[ADR-0003](../adr/ADR-0003-dual-fingerprint.md)）。
5. **策略评估**（03 §判定流程）基于规范化后 finalArgs。
6. **审批**：弹窗展示人可读参数与影响范围；决策（含 execHash）写 `approvals` 表。
7. **执行器校验** `sha256(JCS(args)) == execHash`，不等拒绝执行（`ErrArgsDrift`）；沙箱/工具执行；after_tool_call hooks **只读**结果，不可改参改结果。

红线：Hook 不能伪造用户审批、不能直接调用工具、不能在审批后改参、after 只读、不能访问主进程敏感内存。

## Hook 系统

**进程内 Go Hook 是 MVP 唯一形态**——所有策略、日志、通知、自定义工具经此注入。**外部进程 Hook 延后至后续阶段**（v2 将其排入阶段 2，v3 移出：进程隔离、资源限制、stdin/stdout JSON-RPC 协议是独立工程量，[ADR-0005](../adr/ADR-0005-mvp-security-downgrade.md)）。Go `plugin` 包不支持 Windows 且与签名公证冲突，永不采用。

| Hook 点 | 时机 |
|---|---|
| `before_model_call` / `after_model_call` | 模型调用前后 |
| `before_tool_call` / `after_tool_call` | 工具执行前后（改参仅 before，白名单字段） |
| `before_approval` | 审批前补充上下文或触发额外校验 |

声明式配置 `hooks.d/*.yaml`，启动时 schema 校验；支持优先级、超时、失败策略（warn / deny / fail_closed）；每个 Hook panic 被 recover，不影响 GUI 与主循环。

## 前端渲染安全基线（Phase 2 硬性项）

WebView 渲染模型输出、工具结果、MCP 返回内容——**都可能是不可信 HTML/Markdown/链接**，是提示注入的最后一公里。

| 项 | 要求 |
|---|---|
| CSP | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'none'` |
| Markdown | 禁 raw HTML（或仅白名单标签） |
| 外链 | 统一"拦截 → 展示完整 URL → 用户确认 → 系统浏览器打开"；禁 `javascript:` / `data:` 链接、禁 `target="_blank"` 直开 |
| 来源标记 | MCP / 网页内容渲染带 `data-trust="untrusted"` 视觉标识 |
| 二进制内容 | MCP 返回的文件/图片默认不自动加载，需点击 |

## 沙箱分期路线（[ADR-0005](../adr/ADR-0005-mvp-security-downgrade.md)）

> v2 把"Linux namespace / macOS Seatbelt / Windows AppContainer 统一接口"当一件事排 2 周——三者能力差异极大（Windows AppContainer 需创建 profile SID + 目录 ACL，成本远超前两者；OpenAI 为 Codex 的 Windows 沙箱专门写过一篇工程博客）。**分期落地，前期靠权限 + 审批兜底：**

| 期 | 平台与手段 | 状态 |
|---|---|---|
| Phase 1–4 | 无 OS 沙箱；靠权限引擎 + 审批 + 备份回滚兜底 | MVP |
| Phase 5 | macOS Seatbelt + Linux Landlock（均 deny-by-default、免 root、先例充分） | 计划 |
| Backlog | Windows AppContainer（独立里程碑、独立 ADR + 安全评审） | 待立项 |

## 备份式回滚（取代 v2 完整快照）

> v2 缺陷：文件系统快照跨平台不可行（Windows VSS / APFS 快照需管理员权限 / btrfs 仅特定文件系统），用户态全量复制对大工作区过慢；且同类产品（Codex、Claude Code）均不做快照，靠沙箱 + 审批兜底。**降级为文件级备份**（[ADR-0005](../adr/ADR-0005-mvp-security-downgrade.md)），Phase 4 落地为版本化备份（[ADR-0016](../adr/ADR-0016-versioned-backup-restore.md)）：

1. **覆盖写**：写入前把当前内容存为版本（数据目录 `backups/<session>/<相对路径>/v%06d-<id>.bak`），再写临时文件 + fsync + 原子 rename；每个 `(会话, 路径)` 保留最近 20 版，备份区软上限 512 MiB，超限**拒绝新的覆盖写并审计**而不是静默丢版本；
2. **新建文件**：不留备份副本，只登记创建时 SHA-256；"撤销创建"要求当前内容哈希仍等于创建时哈希，然后把文件**移入** `discarded/<相对路径>/<id>.trash` —— 不 unlink；
3. **恢复**：先把当前内容变成新版本（`pre_restore` 安全副本），再把选定版本复制回目标；恢复本身因此可再撤销；仅非运行态会话可恢复/撤销；
4. **git 集成**：工作区是 git 仓库时，回合结束提示可用 git 兜底比对；
5. 完整快照（增量、跨平台）留 Backlog，立项须 ADR。

写后校验失败（重读哈希与预期不符）时按同一语义自动处置：覆盖写回滚到备份版本，新建移入备份区，并记 `backup/verify_fail` 审计。

## 单实例锁与桌面集成（跨平台修正）

> v2 写的 `flock` + `~/.config` 是 POSIX 假设，Windows 不成立。

- 锁文件路径统一 `os.UserConfigDir()/happywork/instance.lock`；
- 锁原语用 `gofrs/flock`（Windows 底层 `LockFileEx`，POSIX `flock`）；
- 第二实例检测到锁 → 通知第一实例聚焦窗口 → 自身退出。聚焦通道实现：聚焦请求文件 + 低频轮询（Phase 1 实测 stdlib `net` 不支持监听 Windows 命名管道——`net` 的 "pipe" 是内存管道，见 pitfalls P-002 与 design/phase-1 §9；若未来延迟不达标再评估 go-winio 依赖）。

## 审计与操作后校验

- 审计日志**追加只读 + 哈希链常开**（Phase 4，[ADR-0015](../adr/ADR-0015-immutable-audit-chain.md)）：`BEFORE UPDATE/DELETE` 触发器 `RAISE(ABORT)`，链头与写入同事务更新，验证器逐行重算 `prev_hash/entry_hash`，可定位首个坏行；Phase 4 之前的行算"遗留未密封"，UI 如实标注而不是谎称全量可信。威胁模型限定为本地完整性护栏：不防拥有数据库完整写权限并重算整链的攻击者，也无外部锚定。
- 覆盖范围是**安全事件与元数据**，不是内容副本：工具判定、审批、持久规则、Hook 结果、备份/恢复/撤销、链验证、启动恢复各写审计；只记 `relPath`、哈希、决策与短原因，不写文件正文、提示词或密钥原文。
- **fail-closed**：放行路径的审计写入失败即拒绝执行工具（拒绝路径仍照常留痕，写失败降级为日志）。
- 每个 turn 生成一个 `trace_id`，与 `turn_id` 一起贯穿 model → tool → hook → backup 记录；GUI 审计面板可按会话查看链状态、验证整链、导出 JSONL + manifest。
- **操作后校验**：写文件后重读目标并比对 SHA-256；不一致即自动回滚（覆盖写）或移入备份区（新建），并记 `backup/verify_fail`。外部进程在"备份→写入→重读"窗口内改同一文件会被判为校验失败并回滚，桌面单用户场景接受这一毫秒级风险。

## 应用更新与发布签名（v3 新增，Phase 4 落地）

安全敏感的桌面应用必须有补丁通道：内置"检查更新"（对比 releases manifest）+ 下载页跳转 + **签名验证**后才提示安装；**不做静默自动更新**（无人值守安装与产品原则冲突）。

发布侧由 `.github/workflows/release.yml` 在 annotated tag `v*` 上完成三平台签名（[ADR-0017](../adr/ADR-0017-release-signing.md)）：Windows Authenticode（exe + NSIS 安装包，带 RFC 3161 时间戳）、macOS Developer ID 签名 + 公证 + staple（.app 与 .dmg）、Linux `dpkg-sig`/`rpmsign` 包签名与 AppImage 分离签名。凭据只存 GitHub Actions Secrets，缺凭据的正式标签**直接失败**，不产出未签名发布物；`workflow_dispatch` 的 dry-run 用 runner 内临时凭据自验，产物标签为 `dry-run-preview`，正式 Release job 只下载 `*-release`。最终 `SHA256SUMS` 与其 `.asc` 由 release job 汇总三平台产物后生成，公钥随 Release 一起发布。

## 测试基线（v3 新增，对应 [standards/05](../standards/05-testing.md)）

| 类别 | 内容 |
|---|---|
| 权限 golden tests | 判定流程表驱动用例集：四维 × 合并规则 × 降级 × 可逆修正，输入输出全固化 |
| 注入用例集 | Markdown 含 `<script>`、`data:` 链接、symlink/junction 路径逃逸、8.3 短名、大小写变体——全部必须"拒绝且留痕" |
| TOCTOU 演练 | 审批后改参被 `ErrArgsDrift` 拦截；Hook 白名单外字段改参被拒 |
| JCS fuzz | canonical JSON 编码一致性模糊测试（浮点/Unicode/嵌套） |
| 审计链 | 固定哈希向量；改单行/删中间行/插队/空哈希插入/链头不一致均定位首个坏行；UPDATE/DELETE 触发器拒绝；遗留段计数；导出 JSONL 可复算 |
| 备份与恢复 | 多版本与保留裁剪、软上限拒绝、恢复前安全副本、恢复后再恢复、撤销创建（改过即拒绝、缺失幂等）、运行态拒绝、越界/敏感路径拒绝 |
| 写后校验 | 故障注入使重读哈希不符 → 覆盖写自动回滚 / 新建移入备份区，并审计 `backup/verify_fail` |
| 审计 fail-closed | 审计写入失败时放行路径被拒绝，拒绝路径仍留痕 |
| 崩溃恢复 | 写入中途被杀后数据可读/可恢复（standards/05 §2.4） |
| 发布流水线 | `v*` 标签三平台签名/验签；dry-run 用临时凭据走同一命令；缺凭据的正式标签失败；版本一致性门禁（配置 + 生成资产） |

## 风险与待决

| 风险 | 对策 |
|---|---|
| 无沙箱期（Phase 1–4）本地破坏 | 权限 + 审批 + 备份三重兜底；shell/删除默认 deny |
| 备份目录本身膨胀 | 每 `(会话, 路径)` 保留 20 版 + 备份区 512 MiB 软上限；超限拒绝新的覆盖写并审计，不静默丢版本；跨会话清理留后续 |
| 备份元数据丢失 | `file_backups` 被删时磁盘版本文件成为孤儿；本阶段不扫描备份目录重建，需人工按路径找回 |
| 真实签名凭据 | A7 条件项：没有证书/公证账号时 dry-run 只证明流水线可用，不得表述为"已签名发布" |
| CSP `style-src 'unsafe-inline'` | 已知妥协；Phase 2 后评估移除（样式预处理成本） |
| Hook 共享地址空间逃逸 | 进程内 Hook 仅白名单来源 + 签名（TOFU 指纹）；外部进程形态立项后隔离 |

待决：Hook 签名密钥签发（倾向本地生成 + 首次加载 TOFU 指纹展示）；Windows AppContainer 立项时机（Backlog，须独立 ADR）。
