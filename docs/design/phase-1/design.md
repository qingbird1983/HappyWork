# Phase 1 施工设计：最小骨架

- 状态：定稿（2026-10-30，随架构基线一并由所有者放行开工）
- 日期：2026-10-30
- 关联：[phase-1-minimal-skeleton.md](../../roadmap/phase-1-minimal-skeleton.md)（范围合同与验收）；[architecture/01](../../architecture/01-overview.md)~[04](../../architecture/04-security-and-hooks.md)；[ADR-0001](../../adr/ADR-0001-adopt-go-wails3.md)、[ADR-0004](../../adr/ADR-0004-path-canonicalization.md)、[ADR-0007](../../adr/ADR-0007-frontend-react.md)、[ADR-0010](../../adr/ADR-0010-sqlite-driver.md)；[research/03](../../research/03-zcode-ui-reference.md)

## 1. 目标与非目标

目标即范围合同验收 A1~A7（三平台构建 / mock 流式 / 会话恢复 / 单实例聚焦 / 工作区边界 / CSP / 门禁四连）。本篇回答"怎么建"：模块切片、接口契约、数据模型、依赖清单与测试设计。

非目标（与范围合同一致）：真实 LLM、审批引擎、MCP、Hook、回滚、沙箱、上下文折叠、多会话并行、视觉样式（灰盒）。

## 2. 技术决策落位

| 决策点 | 结论 | 依据 |
|---|---|---|
| SQLite 驱动 | `modernc.org/sqlite`（纯 Go，免 CGO），全项目 `CGO_ENABLED=0` | ADR-0010 |
| Wails 版本 | `github.com/wailsapp/wails/v3` **v3.0.0**，钉 minor（v3.0.x） | ADR-0001；2026-10-30 查询 v3 稳定线最新为 v3.0.0 |
| 前端栈 | React + TypeScript + Vite；灰盒零视觉投入 | ADR-0007、roadmap Backlog |
| Go module 路径 | `github.com/happywork/happywork`（仓库未设远端，占位路径，首次发布前不改） | standards/01 |
| Go 版本线 | go 1.26（wails v3 要求 ≥1.26；本机 1.27） | research/01 |
| 前端服务绑定方式 | Phase 1 用 `@wailsio/runtime` 的 `Call.ByName` 字符串绑定，跳过代码生成链路；Phase 2 接入 `wails3 generate bindings` 时切换为生成式 | 灰盒期降低工具链复杂度 |

## 3. 代码结构（Phase 1 实际切片）

```
cmd/agent/main.go            只装配：锁 → 库 → 管理器 → 桌面壳
internal/desktop/            Wails 唯一接触面：app 装配、Platform/Emitter 适配、Service 绑定
internal/agent/              会话管理器 + Actor 循环（Phase 1 状态子集）+ 事件批量 flush
internal/model/              Provider 接口与消息类型
internal/model/mock/         脚本化确定性 Provider（读 fixtures/）
internal/tool/               工具注册表 + read_file 实现 + ToolMeta
internal/workspace/          工作区根 + 路径规范化管线 + 边界判定（ADR-0004；Phase 3 收编入权限引擎）
internal/storage/            SQLite 打开/迁移/会话与消息存取（驱动引入点唯一文件）
internal/audit/              审计写入（Phase 1 仅 append 行，不可篡改机制在 Phase 4）
internal/singleinst/         单实例锁 + 聚焦通道（flock + named pipe/AF_UNIX）
migrations/                  0001_init.sql + embed 入口（迁移幂等）
frontend/                    React+TS 灰壳：三栏信息结构 + CSP
fixtures/mock/*.json         mock Provider 剧本样例
```

依赖方向遵循 architecture/01：`desktop → agent → {model, tool, storage, audit}`；`tool → workspace`；无反向、无环。Wails 类型不越过 `internal/desktop`。

## 4. 数据模型（migrations/0001_init.sql）

| 表 | 关键列 | 说明 |
|---|---|---|
| sessions | id TEXT PK；title；workspace_root；state；created_at/updated_at | state 取状态机子集枚举 |
| messages | id TEXT PK；session_id FK；seq；role CHECK(user/assistant/tool/system)；content；UNIQUE(session_id,seq) | transcript 重放依据（A3） |
| tool_calls | id TEXT PK；session_id FK；name；args_json；result_json；status | Phase 1 只写不读（Phase 2 起展示） |
| audit_log | 自增 PK；session_id；action；decision；reason；created_at | 越界拒绝等留痕（A5） |
| events | 自增 PK；session_id；type；payload_json；created_at | 事件流水（观测与排查） |
| kv | key TEXT PK；value | schema 版本等元数据 |

迁移框架：`migrations/*.sql` 按文件名序执行，`kv.schema_version` 记录已应用版本，重复启动幂等。连接参数：`journal_mode=WAL`、`busy_timeout=5000`、`foreign_keys=ON`。库文件位置：`os.UserConfigDir()/happywork/happywork.db`。

## 5. 事件契约（前端 ⇄ 后端）

Wails 服务 `desktop.SessionService`（JS 侧 `Call.ByName("desktop.SessionService.X")`）：

```go
ListSessions() ([]SessionMeta, error)
CreateSession(workspaceRoot string) (Session, error)
Send(sessionID, text string) error      // 异步触发一轮；结果经事件回流
Cancel(sessionID string) error
Transcript(sessionID string) ([]Message, error)
```

事件（`Emit` 下行，前端 `Events.On` 订阅）：

| 事件 | payload | 说明 |
|---|---|---|
| `session:state` | `{sessionID, state}` | 状态机每次迁移 |
| `session:delta` | `{sessionID, text}` | 批量合并后的文本增量 |
| `session:tool` | `{sessionID, name, status, detail}` | 工具开始/结束（灰型单行） |
| `session:done` | `{sessionID, finalState}` | 一轮终止（completed/error/cancelled） |

批量 flush 规则（architecture/02）：delta 进入缓冲，40ms tick 或终态时统一 flush；单事件仍是完整合法 JSON。所有事件同写 `events` 表（写路径经 storage，读排障）。

## 6. Agent 循环（Phase 1 状态子集）

完整状态机见 architecture/02；Phase 1 实现：

```
idle → thinking → (running_tool ⇄ thinking)* → completed | error | cancelled
```

- `awaiting_approval / paused / persisting / interrupted` 不实现；启动时把处于非终态的存量会话标记为 `interrupted`（崩溃恢复的最小占位，Phase 2 补齐语义）。
- 每回合：取历史（含系统提示与工具定义）→ `Provider.Stream` → delta 缓冲 → `tool_calls` 逐个执行 → 结果以 `role=tool` 回喂 → 回合数 +1；`MaxTurns=50` 触顶转 error。
- 持久化时机：assistant 文本与工具结果在产生时即写库（write-through，无独立 persisting 态）。
- 取消：`Cancel` 关闭会话 ctx；循环在下一个检查点（chunk 到达 / 工具执行前）退出转 `cancelled`，已产生的部分 assistant 文本保留。
- 并发约束：同一会话 `Send` 在进行中时拒绝重入（manager 持有运行标志）；不引入全局锁。

## 7. Mock Provider 行为定义

接口（internal/model）：

```go
type Chunk struct {
    Delta    string     // 文本增量
    ToolCall *ToolCall  // 非空表示一次完整工具调用（mock 不做参数分片）
    Done     bool
    Err      error
}
type Provider interface {
    Stream(ctx context.Context, msgs []Message, tools []ToolSpec) <-chan Chunk
}
```

Mock 实现：构造时给定**剧本**（`fixtures/mock/*.json`），每次 `Stream` 消费下一段剧本；分片按固定 cadence（默认每 10ms 一片）发送，可注入加速用于测试。剧本段类型：

| 段类型 | 字段 | 用途 |
|---|---|---|
| say | text, chunkWords | 普通流式回复（按词切分片） |
| call_tool | name, arguments | 发起一次工具调用 |
| say_tool_echo | template | 读取 msgs 中最近 tool 结果并按模板复述（验证回喂链路） |
| fail | message | 流中途报错（error 分支） |

内置剧本（fixtures/mock/）：`greet`（问候）、`read_file`（call_tool → say_tool_echo，A2/A5 主链路）、`long`（300 片，验证批量 flush 无逐片卡顿）、`boom`（中途失败）。会话默认剧本可配置（kv 表 `mock.script`），缺省 greet。

## 8. read_file 与工作区边界

- 参数：`{"path": "..."}`（字符串，相对或绝对）。结果：`{"content", "size_bytes", "truncated"}`；上限 2 MiB，超出截断并置 `truncated=true`。
- 路径先过规范化管线（ADR-0004 五步）：join → `Clean` → `EvalSymlinks` → Windows 大小写归一 → 拒绝段内含 `~<数字>` 形态的 8.3 短名（启发式拒绝，不尝试展开）。
- UNC（`\\`/`//` 开头）与盘符相对路径（`C:foo`）直接拒绝。
- 边界判定：规范化结果必须等于根或位于根 + 分隔符之下（Windows 用大小写不敏感前缀比较）。越界返回 `ErrOutsideWorkspace`，**写 audit_log（decision=deny）后**再向模型回错误（A5 留痕）。
- 符号链接/junction 逃逸由"规范化后复检边界"拦截；文件不存在返回普通错误（不留审计痕）。
- Phase 1 无权限引擎：read_file 仅做上述**硬编码边界检查**（范围合同明确），Phase 3 收编。

## 9. 单实例锁与聚焦

- 锁：`os.UserConfigDir()/happywork/single.lock`，`gofrs/flock` 独占持有，进程存活期不释放。
- 聚焦通道：主实例监听——Windows 命名管道 `\\.\pipe\happywork-focus`；其余平台 `configDir/happywork/focus.sock`（AF_UNIX）。第二实例获锁失败 → 向通道写 `focus` → 主实例 `FocusMainWindow()` → 第二实例退出（A4）。
- 聚焦实现走 `internal/desktop` 的 Platform 接口（architecture/02），锁包本身不依赖 Wails。

## 10. 前端壳（灰盒）

信息结构参照 research/03（ZCode 观察），灰型三栏：

```
┌──────────┬───────────────────────────────┐
│ 会话列表  │ transcript（user/assistant/tool 灰型块）│
│ +新建    │                               │
│          ├───────────────────────────────┤
│          │ 工作区路径输入 + 输入框 + 发送/停止      │
└──────────┴───────────────────────────────┘
```

- 渲染安全基线（A6）：模型文本一律 `textContent` 注入，禁 `innerHTML`；`index.html` 顶部 CSP meta：`default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ipc://* http://ipc.localhost; img-src 'self' data:`（`connect-src` 以 v3.0.0 实际 IPC 通道为准，联调时核对）。
- Markdown 富渲染、审批弹窗、设置页：不做（Phase 2/3）。
- 工具块：单行灰条（名称 + 状态），无展开——信息结构在 Phase 2 对齐 research/03。
- 质量门禁：`tsc --noEmit` + eslint（typescript-eslint + react-hooks）+ prettier，进 CI（standards/01）。

## 11. 依赖清单（登记 standards/04 附录 A）

Go（3 个直接依赖）：`github.com/wailsapp/wails/v3 v3.0.0`、`modernc.org/sqlite v0.x`、`github.com/gofrs/flock v0.x`。

npm 运行时（3 个）：`react`、`react-dom`、`@wailsio/runtime`。
npm 开发：`typescript`、`vite`、`@vitejs/plugin-react`、`eslint`、`@eslint/js`、`typescript-eslint`、`eslint-plugin-react-hooks`、`prettier`。

规则：超出清单的依赖一律先走 standards/04 §5 准入再引入；lockfile 入库。

## 12. 测试设计

| 层 | 内容 |
|---|---|
| workspace | 表驱动规范化：相对/绝对/大小写/越界/UNC/8.3/`C:foo`/symlink 逃逸 |
| tool | read_file 正常/越界拒绝+审计行/不存在/超限截断 |
| storage | 迁移幂等（连开两次）、会话与消息往返、transcript 按 seq 有序 |
| agent | mock 驱动：状态迁移序列、工具结果回喂、取消中途退出、MaxTurns 触顶、delta 批量后事件数 ≪ 分片数 |
| mock | 剧本确定性（同剧本两次运行输出一致） |
| singleinst | 二次获锁失败 + 聚焦消息送达（按 GOOS 分支覆盖） |

门禁：`gofmt` / `go vet` / `golangci-lint run` / `go test ./...` / `scripts/check-size` / 前端 `prettier --check` + `eslint` + `tsc --noEmit`。CI 矩阵覆盖 linux/macos 构建（A1），Windows 构建本机验证；macOS 公证演练按范围合同由所有者侧执行。

## 13. 验收映射

| 验收 | 实现与验证方式 |
|---|---|
| A1 三平台构建 | CI 矩阵 `wails3 build`；Windows 本机执行；macOS 公证演练所有者侧 |
| A2 mock 流式 | `read_file` 剧本 + `long` 剧本；事件批量 flush |
| A3 会话恢复 | Transcript 接口 + messages 重放；重启后 UI 拉取 |
| A4 单实例聚焦 | singleinst 集成路径，Windows 实测 |
| A5 工作区边界 | workspace 边界拒绝 + audit_log 留痕（UI 可见失败提示） |
| A6 CSP | textContent 注入 + CSP meta；手工注入用例验证 |
| A7 门禁四连 | 本地 + CI 全绿 |

## 14. 风险

| 风险 | 缓解 |
|---|---|
| wails v3.0.0 API 与研究时点记忆有偏差 | 一切以 `wails3 init` 生成模板与官方文档为准；差异收敛在 internal/desktop |
| Windows 命名管道 stdlib 支持异常 | 备选：聚焦通道降级为锁文件心跳 + 轮询（记录 pitfalls） |
| webkitgtk CI 环境缺失 | linux job 允许首期显式标红驱动修复；打包方式本阶段评估 |
| `Call.ByName` 字符串拼写错误无编译期检查 | 服务方法名集中定义在一个 TS 常量文件 |

## 更新记录

- 2026-10-30：定稿。
