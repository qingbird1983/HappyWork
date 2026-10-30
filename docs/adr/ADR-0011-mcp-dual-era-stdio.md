# ADR-0011：MCP stdio 客户端同时兼容两代协议

- 状态：定稿（2026-10-30；随 Phase 2 立项放行）
- 日期：2026-10-30
- 关联：[设计稿](../design/phase-2/mcp.md)；[architecture/04](../architecture/04-security-and-hooks.md) §Hook/扩展；[research/02](../research/02-deepseek-api.md)（同期外部事实纪律）

## 背景

MCP 在 2026-07-28 发布第 5 版规范，做了破坏性调整：协议从"有状态长连接"转为**无状态核心**——移除 `initialize` 握手、移除会话 ID；每个请求在 `params._meta` 中自描述
（`io.modelcontextprotocol/protocolVersion`、`clientInfo`、`clientCapabilities`）；响应必须带 `resultType`（`complete` / `input_required`）；新增 `server/discover`；多轮往返改为 MRTR。

与此同时，现存绝大多数本地 server 仍按旧版（`2025-11-25` 及更早）实现 `initialize` 握手。HappyWork 的 Phase 2 必须接入本地 MCP 生态；只支持新版会"一个都连不上"，只支持旧版会很快过时。stdio 是本期唯一传输（远程 MCP 在 Backlog）。

## 决策

1. **双代兼容，现代优先**：启动每个 server 后先发现代 `server/discover`（带新版 `_meta` 三元组，5s 超时）。
   - 成功 → modern 模式：不握手；每个请求带 `_meta`；严格校验响应 `resultType`。
   - 返回 `-32601`（method not found）→ legacy 模式：走 `initialize`（`protocolVersion: 2025-11-25`）+ `notifications/initialized`；请求不带 `_meta`；不要求 `resultType`。
   - 两者都失败/超时 → 该 server 本次运行禁用并提示，不猜测、不重试到无限。
2. **只实现 `tools/*`**：resources/prompts、sampling/roots/elicitation、MRTR（`input_required`）明确报错并留痕，不静默降级。
3. **stdio 分帧按规范**：UTF-8、逐行 JSON-RPC；stdout 只走协议，stderr 只做诊断（截断入日志）。HTTP 专属的版本/方法头不适用。
4. **协议事实以官方规范为准**：实现时以当日 `modelcontextprotocol.io/specification/2026-07-28` 与旧版规范交叉核对；fixture 记录抓取日期。

## 理由

- 生态过渡期，双代兼容是唯一能"现在可用、未来可退"的方案；探测成本一次 5s，失败可解释。
- 现代模式严格、旧版宽松，避免把"新版 server 不守规范"静默放行；失败即禁用符合 fail-closed。
- 不引入 MCP SDK：SDK 会同时绑定传输、版本与遥测策略，且双代兼容仍需自己写探测；标准库实现约数百行，可控。

## 后果

- 积极：本地旧 server 立即可用；新版 server 无需等生态迁移；协议差异集中在一个包内（`internal/mcp`），远程 MCP 后续另立 ADR。
- 代价/义务：双模式测试矩阵；规范继续演进时需更新探测（记录触发条件：新版成为绝对主流或旧版握手被移除）；`resultType`/MRTR 的完整支持留后续。
- 重新评估条件：MCP 发布不兼容的新主版本；本地 server 生态完成迁移；需要远程 MCP（届时写新 ADR）。

## 来源

- MCP 规范 2026-07-28（`modelcontextprotocol.io/specification/latest`，2026-10-30 核对）：stdio 逐行分帧、无握手、`_meta` 版本元数据、`resultType`、`server/discover`、tools 分页与内容类型。
- 旧版生命周期（`initialize` + `notifications/initialized`）对照同一官方站点历史版本。
