# Phase 2 验收报告

- 状态：定稿（2026-10-30；CI run 41054482312 全绿）
- 日期：2026-10-30
- 关联：[phase-2-controlled-execution.md](../../roadmap/phase-2-controlled-execution.md) §5（A1~A8）；[design.md](design.md)；[mcp.md](mcp.md)；[ADR-0011](../../adr/ADR-0011-mcp-dual-era-stdio.md)；[CI run 41054482312](https://github.com/qingbird1983/HappyWork/actions/runs/41054482312)
- 分支：`feat/phase2-controlled-execution`
- 验收方式：所有者 2026-10-30 授权 AI 会话自跑真机验收。证据来源：自动化测试、真机截图、SQLite 落库、启动日志、门禁输出；真机产物保存在本地 `.task/phase2-acceptance/`（不入库）。

## 1. 验收结论

**8/8 通过，Phase 2 达成。** 两项一票否决均通过：A1 真实 DeepSeek 完成读/列目录 + 审批写入闭环，usage 与费用入库，日志/数据库/transcript 未出现 key；A8 本地门禁、Windows 构建与三平台 CI（check + windows/macos/ubuntu build）全绿。

| # | 验收场景 | 结果 | 证据 |
|---|---|---|---|
| A1 | 真实模型闭环（一票否决） | ✅ | 手工真机测试：2 次工具调用、1740 tokens、$0.000457；GUI 会话：审批后写入 `hello.txt`（20 字节），usage 1596 tokens / $0.000224712；key 泄漏断言通过 |
| A2 | 审批交互 | ✅ | 真机写文件弹窗（字段、预览、once/session、deny 反馈）；单测覆盖 once/session/deny/超时暂停重弹/取消/过期 ID |
| A3 | 写文件与敏感路径 | ✅ | 真机写成功；单测覆盖覆盖前备份、临时文件 + fsync + rename、越界/UNC/8.3 拒绝、`.env`/`.git/`/私钥硬拒并落审计 |
| A4 | MCP stdio | ✅ | 真机发现并调用 `demo/read_note`，结果 `trust=untrusted` 且逐条审批；单测覆盖双代协议、分页、schema 白名单、环境隔离、超时重启上限、大结果附件化 |
| A5 | 崩溃恢复 | ✅ | 真机在审批等待中强杀 → 重启显示 interrupted → 继续 → 注入"结果未知" → 完成；新增集成测试覆盖 `running_tool` 强杀 → `tool_calls.status=unknown` |
| A6 | 渲染安全 | ✅ | vitest 10 项注入断言 + 静态门禁；真机 MCP 注入 `<script>` 以纯文本显示，模型将其当数据处理 |
| A7 | 预算 | ✅ | 单测覆盖轮次/工具数/token/时长/费用五条上限，均为 paused + 一次宽限，无静默截断；UI 按原因显示"继续（一次宽限）/继续（重新审批）" |
| A8 | 门禁（一票否决） | ✅ | 本地 fmt/vet/lint/test/check-size/render-safety + 前端五连全绿；CI run 41054482312 的 check 与三平台 build 全部 success |

## 2. A1 明细与费用

### 2.1 自动化真机测试（manual 标签，默认不进 CI）

```
go test -tags manual -run TestDeepSeekLive -v ./internal/agent
```

结果：`live deepseek ok: model=deepseek-flash tools=2 tokens=1740 cost=$0.000457`。测试断言：至少一次工具循环、usage 为真实值（非估算）、transcript 与日志中检索不到 `DEEPSEEK_API_KEY`。

### 2.2 GUI 全链路

- 会话 1：提示写 `hello.txt` → 弹窗展示目标路径/操作/影响/预览 → "仅本次允许" → 文件写入成功（20 字节，内容与请求一致）→ 模型确认完成。
- 会话 2：同一提示在审批弹窗期间强杀，用于 A5；恢复后模型基于"结果未知"继续。
- usage（`events` 表）：会话 1 累计 1596 tokens / $0.000224712；会话 2 累计 877 tokens / $0.000210672。
- 密钥排查：对两套隔离数据目录的 SQLite、WAL、应用日志以及仓库工作区做 `sk-` 形态扫描，均无命中；`TestDeepSeekLive` 另有显式断言。

### 2.3 费用量级

真机验收合计约 **$0.0009**（live 测试 $0.000457 + 两次 GUI 会话 $0.000435），远低于 Phase 2 单会话 $2 预算。

## 3. A2~A7 证据索引

### A2 审批交互

- 真机：`ds-approval.png`（写文件弹窗）、`mcp-approval.png`（外部工具弹窗，只有"仅本次允许"）。
- 落库：`approvals` 表写入 `tool_ref=builtin://write_file`、`scope=once`、`match_key=builtin://write_file|write|<工作区目录>`；MCP 调用写入 `tool_ref=mcp://demo/read_note`、`scope=once`。
- 单测：`TestWriteApprovalOnce`、`TestWriteDenyWithFeedback`、`TestWriteSessionScope`、`TestApprovalTimeoutPauseResume`、`TestCancelDuringApproval`；过期/重复 ID 返回 `ErrStaleApproval`，参数漂移返回 `ErrArgsDrift`。

### A3 写文件与敏感路径

- 真机：`hello.txt` 内容与请求一致；写操作经审批后原子落盘。
- 单测：`TestWriteFileCreateAndOverwrite`（覆盖前副本）、`TestWriteFileRejections`、`TestWriteFileRefusesUnbackedOverwrite`、`TestWriteFileSensitiveError`、`TestSensitiveTable`、`TestSensitiveReadDenied`（审计含 `sensitive`）；越界/UNC/8.3 由 workspace 测试覆盖。

### A4 MCP

- 真机：配置 Python stdio server，发现 `read_note`；弹窗来源显示 "MCP: demo（内容不可信）"；结果写入 transcript 且 `trust=untrusted`；模型回答中 `<script>` 原样作为文本引用，未执行。
- 单测：`TestModernProbeDiscoveryAndCall`、`TestLegacyProbeAndCall`、`TestPagination`、`TestEnvironmentIsolation`（不透传 API key）、`TestCallTimeoutKillsAndRestarts`、`TestImagePlaceholderAndErrors`、`TestConfigLoadValidation`、`TestManagerStartAndSnapshot`。
- 防护：MCP 子进程使用 `HideWindow`；真机验证 Python 子进程 `MainWindowHandle=0`（无控制台窗口）。仅强制终止路径调用 `taskkill /T /F`；stderr 按行限长，防止日志与内存失控。
- 故障可见性：配置一个不可用的 server 启动，应用正常运行并在顶栏显示"MCP 异常"（`mcp-broken4.png`），不会静默降级。

### A5 崩溃恢复

- 真机：审批弹窗期间强杀进程 → 重启日志 `marked 1 interrupted session(s)` → UI 显示"上次中断" → 继续 → transcript 注入"应用中断，执行结果未知" → 模型说明不确定性并完成；放弃路径单测 `TestRecoveryDiscard`。
- 新增集成测试 `TestCrashDuringRunningToolMarksUnknownThenResume`：模拟 `running_tool` 落库状态 → 重启标记 `interrupted` + `tool_calls.status=unknown` → 新 Manager 重新水化 → Resume 注入未知结果并完成。
- 语义修正：重启只把 `thinking/running_tool/persisting/awaiting_approval/paused` 改为 interrupted，空闲会话保持 idle（`TestMarkInterruptedNonTerminal`）。
- 说明：真机强杀发生在审批等待态（工具尚未执行，故无 tool_calls 行）；`running_tool` 强杀路径由上述集成测试覆盖。

### A6 渲染安全

- 前端只经 React 文本节点渲染；`render.test.tsx` 10 项覆盖 `<script>`、`<img onerror>`、`javascript:`、`data:`、属性注入、MCP 不可信徽章等。
- `scripts/check-render-safety.sh`：禁止 `dangerouslySetInnerHTML`/`innerHTML`/`outerHTML`/`document.write`，校验 CSP 指令齐全（`frame-src`/`object-src` 为 `none`，`connect-src` 含 Wails IPC）。
- 真机：MCP 返回的注入文本在界面显示为纯文本（`mcp-result.png`），无脚本执行、无可点击链接。

### A7 预算

- 单测：`TestMaxTurnsExhausted`、`TestBudgetTokenPauseAndGrace`、`TestBudgetCostPauseAndGrace`、`TestBudgetToolCallsPause`、`TestBudgetWallClockPause`。
- 行为：超限 → `paused` + `session:paused` 事件；继续 → 对应项一次性宽限（+10 轮/次、+10 工具、+500k token、+10 分钟、+$0.50）；再次超限 → 明确失败，不静默截断。
- UI：暂停横幅按 `reason` 区分"继续（一次宽限）"与"继续（重新审批）"。

### A8 门禁

| 项目 | 结果 |
|---|---|
| `gofmt` | 本阶段文件 0 差异（Phase 1 两文件为本地 CRLF 检出，LF 内容经核对无格式差异） |
| `go vet ./...` | 通过 |
| `golangci-lint run ./...` | 0 issues |
| `go test ./...` | 13 个包全部通过 |
| `bash scripts/check-size` | 通过（docs 35 篇，.agents 9/40） |
| `bash scripts/check-render-safety.sh` | ok |
| 前端 lint / typecheck / format / test / build | 全部通过（vitest 10/10） |
| `wails3 build`（Windows） | 通过，产物 `bin/happywork.exe` |
| 三平台 CI | ✅ [run 41054482312](https://github.com/qingbird1983/HappyWork/actions/runs/41054482312)：check + windows/macos/ubuntu build 全 success |

## 4. 偏差与遗留

| 项 | 说明 | 去向 |
|---|---|---|
| 生成式绑定仍不可用 | `wails3 generate bindings` 输出 `1 Package, 0 Services, 0 Methods`，并提示根目录无 Go 文件；internal 服务未被识别 | 保留 `Call.ByName`（集中在 `frontend/src/api.ts`）；绑定生成留待 Wails 修复或 Phase 3 评估 |
| 真机强杀点在审批等待态 | 未做第二次 GUI 的 `running_tool` 强杀 | 已用集成测试覆盖同一条启动恢复代码路径；如需 GUI 证据可后续补跑 |
| 会话作用域仅单测 | 真机只验证了 once | session 作用域由 `TestWriteSessionScope` 覆盖（同目录第二次不再询问 + 授权查询） |
| 预算真机未逐项触发 | 真机用量远低于上限 | 五条上限均由单测覆盖；真机仅观察 usage/cost 展示 |

## 5. 实现期间修复的缺陷

- 终态未及时清除 `running`：上一轮完成后立即 Send 会被误判忙碌。
- 审批快照提前清除：执行前校验误报 `ErrArgsDrift`。
- 大结果附件路径重复拼接 `attachments/attachments`。
- MCP 子进程弹出控制台窗口（`HideWindow` 修复，真机验证句柄为 0）。
- MCP 调用的 `matchKey` 未落库（改用权限裁决返回值兜底）。
- 重启把空闲会话误标 interrupted（改为仅活动状态）。
- `taskkill` 无条件执行、stderr 单行无上限、MCP 启动失败未在 UI 提示、暂停按钮文案不分原因、空数据目录仍尝试加载 MCP 配置——已一并修复。
- 测试基础设施：假时钟注册竞态、恢复测试对"thinking"中间态的脆弱断言、预算事件与状态更新的时序断言——已改为等待事件/离开 interrupted。

## 6. 依赖准入核对

新增 Go 依赖：无（DeepSeek 走 `net/http`，MCP 走 `os/exec` + `encoding/json`）。新增 npm 开发依赖：`vitest` 5.0.5（已登记 standards/04 附录 A）。未使用第三方 MCP/DeepSeek SDK。
