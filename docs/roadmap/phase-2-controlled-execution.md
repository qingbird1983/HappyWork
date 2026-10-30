# Phase 2：受控执行闭环

- 状态：定稿（2026-10-30；所有者放行，随立项生效）
- 日期：2026-10-30
- 关联：[00-roadmap](00-roadmap.md) §1；[architecture/02](../architecture/02-core-loop-and-data.md)~[04](../architecture/04-security-and-hooks.md)；[ADR-0008](../adr/ADR-0008-deepseek-test-provider.md)、[ADR-0011](../adr/ADR-0011-mcp-dual-era-stdio.md)；[research/02](../research/02-deepseek-api.md)、[03](../research/03-zcode-ui-reference.md)；施工设计 [design.md](../design/phase-2/design.md)、[mcp.md](../design/phase-2/mcp.md)

## 1. 目标

Phase 1 证明"壳能跑"；Phase 2 证明"模型能在人的控制下真做事"。

一句话验收：让 DeepSeek 在工作区内完成一次"读 → 写 → 调 MCP 工具"的任务；每次写入与每个第三方工具调用都按预期被拦截、询问、记录；拒绝与中断都不会把会话留在不确定状态。

## 2. 范围

### 做

- **真实 Provider**：DeepSeek（OpenAI 兼容端点）流式文本、function calling、usage 计费；key 只从环境变量 `DEEPSEEK_API_KEY` 读取，日志/数据库/transcript 全程脱敏；保留 mock，离线演示与自动化测试仍用 mock。
- **Actor 完整状态机**：`awaiting_approval / paused / persisting / interrupted` 全部落地；审批等待期间取消可即时生效；审批超时进入暂停而不是静默拒绝；崩溃后用户二选一（继续 / 放弃）。
- **本地工具**：在 `read_file` 之外增加 `list_dir`（只读）与 `write_file`（仅工作区内；临时文件 + 原子 rename；覆盖前保留单文件副本）。
- **审批闭环（四维模型的前三维）**：DIM1 决策（allow/ask/deny）、DIM2 作用域（once / session + matchKey）、DIM3 条件（工作区路径前缀）；弹窗按 [research/03](../research/03-zcode-ui-reference.md) 三要素（选项 label+description 成对、作用域明示、deny 带反馈）；决策与参数快照写入 `approvals` 表。DIM4 限额与 persistent 规则随 Phase 3 权限引擎补全。
- **MCP stdio 客户端**：本地多 server 配置、工具发现与调用、schema 白名单、进程生命周期与重启上限；MCP 工具结果一律视为不可信，并逐条审批（不提供 session 作用域）。
- **渲染安全**：不可信内容纯文本渲染（不渲染 Markdown/HTML，不生成可点击链接）、CSP 收紧、来源徽章、二进制占位；A6 注入用例自动化（React 服务端渲染断言 + 静态门禁）。
- **预算**：MaxTurns 50 / MaxToolCalls 100 / 5M tokens / 30 分钟 / $2（flash 折算）；任一超限进入暂停询问，不静默截断；usage 入库并可在 UI 查看。
- **敏感文件硬拒**：工作区内 `.git/`、`.env*`（示例文件除外）、`.ssh/`、常见私钥/证书扩展名，读写一律拒绝并审计（模型会把内容发往云端，这条不能只靠审批）。
- **生成式绑定尝试**：限时尝试 `wails3 generate bindings`；不成功则保留 `Call.ByName` 并记录原因，不阻塞阶段验收。

### 不做（明确排除）

- 完整权限引擎：工具映射表 golden tests、JCS execHash、persistent 规则、限额（DIM4）、角色预设 → **Phase 3**
- TOCTOU 管道与进程内 Hook → **Phase 3**
- 正式备份式回滚与不可变审计 → **Phase 4**（`write_file` 只做覆盖前单文件副本，不宣称可回滚）
- 删除、shell、网络工具；工作区外写入 → 本期不放行（映射表中删除/shell 的默认 deny 行 Phase 3 才存在）
- OS 沙箱、三平台签名 → **Phase 4/5**
- 远程 MCP、MCP resources/prompts → Backlog
- Markdown/富文本渲染、设置界面、多会话并行、上下文自动摘要/折叠 → Backlog（1M 上下文 + 预算兜底；见 §6）

## 3. 开工前置条件

- [architecture/01](../architecture/01-overview.md)~[04](../architecture/04-security-and-hooks.md) 定稿 ✓（2026-10-30）
- `docs/design/phase-2/` 定稿（[design.md](../design/phase-2/design.md) + [mcp.md](../design/phase-2/mcp.md)）与 [ADR-0011](../adr/ADR-0011-mcp-dual-era-stdio.md) 定稿
- 本机 `DEEPSEEK_API_KEY` 已配置 ✓（2026-10-30 仅核对存在性，未读取值）
- Phase 1 已打 tag `phase-1.end`，main 绿 ✓
- 所有者过目本文件与设计稿（知情权与叫停权，[standards/03](../standards/03-documentation.md) §1）

## 4. 交付物

| 类别 | 清单 |
|---|---|
| 代码 | `internal/model/deepseek/`、`internal/mcp/`、`internal/permission/`（Phase 2 门卫）、`internal/agent/`（actor/approval/budget/recovery）、`internal/tool/{listdir,writefile}.go`、`internal/storage`（迁移 0002 + approvals）、`frontend/`（审批/暂停/恢复/工具块/安全渲染） |
| 文档 | 本文件、`design/phase-2/{design,mcp}.md`、`report.md`、ADR-0011 |
| 其他 | DeepSeek SSE 与 MCP 协议 fixtures；Go 测试 MCP server；`scripts/check-render-safety.sh`；`migrations/0002_phase2.sql` |

## 5. 验收场景

- [ ] A1 真实模型闭环（一票否决）：配置 `DEEPSEEK_API_KEY` 后，DeepSeek 流式回复并依次调用 `read_file` / `list_dir` / `write_file`；usage 入库；在日志、数据库、transcript 中检索不到 key；报告披露费用量级。
- [ ] A2 审批交互：写文件弹窗展示选项 label+description、作用域句、影响范围；once 只执行一次；session 同目录第二次不再询问；deny 带反馈回给模型并继续；5 分钟超时 → paused → Resume 重弹同一参数；Cancel 生效；过期 approvalID 被拒。
- [ ] A3 写文件与敏感路径：工作区内写成功；覆盖前副本存在；模拟中断不产生半文件；工作区外 / UNC / 8.3 路径拒绝并写审计；`.env`、`.git/`、私钥类文件读写被硬拒并留痕。
- [ ] A4 MCP stdio：配置的 server 工具被发现与调用；结果带 untrusted 来源；大结果附件化；超时 / 崩溃触发重启上限（3 次 / 10 分钟）后禁用并提示。
- [ ] A5 崩溃恢复：`thinking` / `running_tool` 中强杀进程；重启显示 interrupted；继续 → 未完成工具补"结果未知"并继续；放弃 → cancelled；`tool_calls.status=unknown`。
- [ ] A6 渲染安全：注入 `<script>`、`<img onerror>`、`javascript:`、`data:` 的输出不执行、不生成可点击链接；CSP 静态检查与 SSR 测试通过；MCP 内容有不可信徽章；图片/附件不自动加载。
- [ ] A7 预算：轮次 / 工具数 / token / 时长 / 费用任一超限 → paused 并给出"继续（一次宽限）"或"停止"；usage 与实际调用记录一致；无静默截断。
- [ ] A8 门禁：三平台 `wails3 build` 全绿（一票否决）；`gofmt / go vet / golangci-lint / go test / check-size` + 前端 `fmt / lint / typecheck / test / render-safety` 全绿；CI 绿。

## 6. 依赖与风险

| 依赖/风险 | 说明与缓解 |
|---|---|
| DeepSeek API 行为漂移 | research/02 冻结事实；接入以当日官方文档为准；SSE / tool_calls 分片有 fixture 测试；异常显式报错、不自动重试 |
| MCP 规范两代并存（2026-07-28 无握手 / 旧版 initialize） | 双代探测与兼容见 [ADR-0011](../adr/ADR-0011-mcp-dual-era-stdio.md)；fixtures + 双模式测试 server |
| MCP server 是不可信本地代码 | 只允许用户显式配置；最小环境变量注入（不透传 API key）；逐条审批；超时/崩溃有上限 |
| 审批疲劳 | 写操作 session 作用域绑 matchKey（目录级）；once 默认；persistent 留 Phase 3 且需二次确认 |
| 无完整回滚 | 覆盖前单文件副本 + 原子写；正式回滚 Phase 4；弹窗明示覆盖风险 |
| 上下文增长 | 单工具结果 > 256 KiB 附件化；1M 上下文 + 预算兜底；折叠/摘要进 Backlog，出现真实压力再立项 |
| 生成式绑定仍不可用 | 限时尝试；失败保留 `Call.ByName`，写入报告，不阻塞验收 |
