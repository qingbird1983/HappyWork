# 02-核心循环、状态与数据

- 状态：定稿（2026-10-30，所有者放行开工）
- 日期：2026-10-30
- 关联：[01-overview](01-overview.md)；[ADR-0002](../adr/ADR-0002-unified-permission-model.md)；[research/01](../research/01-v2-design-review.md)

## 定位与职责

Agent 控制面的运行时核心：**以确定性代码推进会话状态**——状态机、回合循环、预算、上下文组装、持久化与崩溃恢复。模型只产生"下一步动作"，本模块决定"动作是否被执行、如何被记录、出错时停在哪"。

## Session Actor：每会话一个命令队列

状态推进、审批等待、取消处理全部收敛到单个 `sessionActor` 的命令队列串行执行。**不用全局锁，不允许并发推进同一会话**——取消、审批超时、恢复的语义由此才确定。

```go
type Command struct {
    Kind    CmdKind   // Submit / Approve / Deny / Cancel / Resume
    Payload any
    Reply   chan error
}

func (a *sessionActor) Loop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            a.shutdown()              // kill 工具子进程、落盘 cancelled
            return
        case cmd := <-a.cmds:
            err := a.handle(ctx, cmd) // 串行推进状态机
            cmd.Reply <- err
        }
    }
}
```

- **审批等待不阻塞主循环**：`select { case dec := <-a.approvalCh; case <-ctx.Done() }`，取消与超时即时响应。
- **取消传播**：`Cancel` → actor `cancel()` → ctx 取消 → 执行器 kill 子进程 → transcript 记 `cancelled`（不是 `error`）→ 已产生副作用的部分触发检查点提示。
- **工具执行串行**：MVP 内同一会话工具调用严格串行；并行须依赖图 + 独立安全评审（Backlog）。

## 状态机（完整转移表）

```
idle --Submit--> thinking
thinking --tool call--> running_tool
thinking --risky call--> awaiting_approval
awaiting_approval --approve--> running_tool
awaiting_approval --deny/feedback--> thinking
awaiting_approval --timeout(默认5min)--> paused
paused --Resume--> awaiting_approval        （v3：一键恢复原审批，参数不变）
paused --Cancel--> cancelled                （v3）
running_tool --result--> persisting
persisting --continue--> thinking
thinking / running_tool --Cancel--> cancelled
thinking --stop--> completed
thinking --unrecoverable--> error
（v3）进程崩溃后重启：thinking / running_tool / persisting / awaiting_approval / paused
      --启动重放--> interrupted
interrupted --用户选择继续--> thinking       （重放当前 turn）
interrupted --用户选择放弃--> cancelled
```

审批超时进入 `paused` 并发桌面通知，**不静默拒绝**——静默拒绝在用户离开时产生大量误拒，损害"审批有意义"本身。审批展示"谁调用、调用什么、最终参数、可能改什么、是否可逆"。

## 回合推进器

```go
func (r *Runner) RunTurn(ctx context.Context, sid string) error {
    s, _ := r.Sessions.Get(ctx, sid)
    for i := 0; i < r.Budget.MaxTurns; i++ {
        if err := r.Budget.ConsumeTurn(s); err != nil { return err }
        resp, err := s.Provider.Generate(ctx, s.BuildRequest())   // 上下文组装见下节
        if err != nil { return r.handleProviderErr(ctx, s, err) }
        if len(resp.Calls) == 0 { return r.finalize(ctx, s, resp.Message) }
        for _, call := range resp.Calls {          // 串行执行（硬约束）
            if err := r.detectLoop(s, call); err != nil { return err }
            out, err := r.executeCall(ctx, s, call) // 完整管道见 04 §TOCTOU
            s.AppendToolResult(call.ID, out, err)
            r.Sessions.SaveTurn(ctx, s)             // 持久化门：每步落盘
        }
    }
    return ErrMaxTurnsExceeded
}
```

超预算默认行为是**暂停并询问**（继续 / 提高预算 / 停止），不是静默截断——静默截断会让 Agent 交半成品却声称完成。

## 预算与循环检测

```go
type Budget struct {
    MaxTurns     int           // 默认 50（v3：20 → 50，真实任务常见 50+ turn）
    MaxToolCalls int           // 默认 100
    MaxTokens    int64         // 会话累计；口径 = provider 返回 usage，缺失时本地估算并标注
    MaxWallClock time.Duration // 默认 30 分钟
    MaxCostUSD   float64       // 按 provider 单价折算
}

// 连续 3 次相同 tool_ref + execHash → 强制中断并询问
func (r *Runner) detectLoop(s *Session, call ToolCall) error {
    if s.repeatCount(call.Fingerprint()) >= 3 { return ErrSuspectedLoop }
    return nil
}
```

已知局限（明示）：参数渐变循环（依次读 file1…fileN）与交替模式检测不到，由 token / 时钟预算兜底。

## 上下文窗口管理（v3 新增）

> 长会话 transcript 必然超过 context window；这是 Agent 产品第一工程问题，v2 缺失，v3 补齐。原则：**组装预算制 + 分级折叠**。

| 层级 | 规则 |
|---|---|
| 每 turn 组装 | system prompt + 会话摘要 + 最近 N 轮完整内容 + 本轮工具结果；组装前按 provider 上限做 token 预算分配 |
| 工具结果（单条） | > 256 KB 存附件，上下文只回传摘要 + hash（v2 既有） |
| 工具结果（历史） | 退出"最近窗口"后折叠为一行引用（`tool_call_id + 摘要 + 附件 hash`），模型可请求展开 |
| 旧轮次压缩 | 接近上限时自动摘要旧轮次；**压缩动作需用户确认**，压缩前后内容均留 transcript |
| 计费口径 | token 以 provider 返回 usage 为准；无 usage 的 provider 本地估算并在 UI 标注"估算" |

## 数据模型与持久化

所有行带 `id`、`session_id`、`turn_id`、`created_at`、`payload`。**transcript 只追加。**

| 表 | 关键字段 | 约束 |
|---|---|---|
| `sessions` | id, title, model_id, mode, status | 状态不得被 GUI 直接改 |
| `messages` | session_id, turn_id, role, payload_json | 只追加 |
| `tool_calls` | tool_ref, match_key, exec_hash, idempotency_key, risk, reversible, decision | 唯一键防重复执行 |
| `approvals` | session_id, turn_id, tool_call_id, tool_ref, exec_hash, decision, scope, decided_by, decided_at, note | 独立审计入口："这次写文件是谁批的"直查此表 |
| `permission_rules` | tool_ref, action, match_key, decision, use_count | 持久授权；GUI 可撤销 |
| `audit_log` | session_id, turn_id, trace_id, action, decision, reason, detail_json, prev_hash, entry_hash | 追加只读（UPDATE/DELETE 触发器 `RAISE(ABORT)`）；`entry_hash` 由前驱哈希链式计算，见 [ADR-0015](../adr/ADR-0015-immutable-audit-chain.md) |
| `audit_chain` | id=1, last_id, last_hash, updated_at | 链头单行表；与 INSERT 同事务更新，验证以 `audit_log` 行为准、不信任链头 |
| `file_backups` | id, session_id, turn_id, rel_path, kind, version, backup_rel/backup_sha256, written_sha256, discarded_rel, restore_of, verified_at/restored_at | 版本化备份元数据，`UNIQUE(session_id, rel_path, version)`，见 [ADR-0016](../adr/ADR-0016-versioned-backup-restore.md) |
| `events` | session_id, turn_id, name, payload_json, trace_id | 审计用，不与 UI 事件混用 |
| `kv` | key, value_json | 轻量状态、persistent 审批规则 |

SQLite 纪律：**连接池上限固定为 1**（`SetMaxOpenConns/SetMaxIdleConns`）——pragma 是按连接生效的，多连接会让 `busy_timeout/WAL/foreign_keys` 只覆盖第一条连接，并让"先读后写"的事务升级成 `SQLITE_BUSY_SNAPSHOT`；序号/版本分配（消息 `seq`、备份 `version`、审计链头）必须在同一事务内完成读+插入，单连接不等于事务（见 `internal/storage/sqlite_write.go`）。WAL 自动 checkpoint 默认 1000 页（按实测调，高频写入有间歇卡顿风险）；备份用在线接口，不复制打开中的文件。并发面由 `sqlite_contention_test.go` 守住（写者 × 读者交错，断言无 BUSY、无唯一约束冲突）。审计追加另有进程内互斥，避免链头竞态。

**迁移与恢复**：迁移幂等可重试（0001→0004）；启动校验 `schema_version`，不匹配拒绝启动并提示升级路径；旧库的审计行没有哈希，验证器计为"遗留未密封"段，首个新行以空前驱开链。会话恢复依赖 transcript 重放。

## 崩溃恢复（v3 新增）

- 启动时扫描运行态会话（见状态机 `interrupted` 转移），由 transcript 重放标记，UI 引导用户二选一：继续（重放当前 turn）或放弃（标 `cancelled`）。
- 崩溃时正在执行的工具，副作用**是否已发生不可知**（文件写一半 / API 已发出无响应）：以幂等键在 `tool_calls` 标记"结果未知"，用户检查实际影响后再决定重试或放弃。
- 备份元数据本身就是恢复线索：覆盖写记录在被标记"已校验"之前仍是未确认版本，新建记录在 `written_sha256` 落库前表示"目标文件未确认存在"；启动恢复把中断的工具调用标出后写 `recovery` 审计，GUI 备份面板把这类版本显示为"未完成/未确认"，不提供恢复入口。
- 完整快照式检查点不存在（Phase 4 是文件级版本备份），恢复时对照 `file_backups` 的版本列表与目标文件当前哈希。

## 可观测性

| 层 | 内容 |
|---|---|
| 实时 UI | 流式文本、工具进度、审批请求；事件驱动，30–50ms 批量 flush |
| 结构化日志 | session_id / turn_id / tool_ref / match_key / exec_hash / risk / reversible / duration_ms / tokens |
| 审计表 | `audit_log`（哈希链、追加只读）+ `approvals` + `tool_calls`；GUI 审计面板可验证整链并导出 JSONL + manifest |
| 追踪 | 每次运行生成 `trace_id`，与 `turn_id` 一起贯穿 model → tool → MCP → hook → backup |

**流式背压**：逐 token 的 Wails 事件在高频下拖垮 IPC；适配层按 30–50ms 聚合 flush，前端慢消费时合并/丢弃中间 delta、只保留最终状态。

## 对外接口：Wails 适配层

```go
// internal/desktop/platform.go —— 所有 Wails 调用收敛于此
type Platform interface {
    Emit(name string, data any)        // 内部批量 flush
    Subscribe(name string, fn func(payload []byte))
    FocusMainWindow() error
    OpenExternal(url string) error     // 调用方须先经用户确认（04 §渲染安全）
}
```

## 风险与待决

| 风险 | 对策 |
|---|---|
| 摘要压缩丢失关键上下文 | 压缩需用户确认；原始内容永久留 transcript 可回看 |
| usage 口径不一致导致预算失真 | 统一口径规则（上表）；估算值显式标注 |
| 崩溃恢复重放引发二次副作用 | 幂等键 + "结果未知"标记 + 用户确认 |
| 高频写入 SQLite 卡顿 | WAL 调参实测；events 批量事务落盘 |

待决：最近窗口 N 轮与各层 token 预算比例（Phase 2 实测定值）；压缩摘要由本会话模型生成还是独立小模型（Phase 2）。
