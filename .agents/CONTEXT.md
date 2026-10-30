# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 2 立项放行 + 受控执行闭环开工）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1（最小骨架）✅ 完成（2026-10-30，tag `phase-1.end`）**：7/7 验收通过，报告见 design/phase-1/report.md
- **Phase 2（受控执行闭环）进行中（2026-10-30 立项）**：范围合同、施工设计、MCP 设计、ADR-0011 已由所有者放行并转定稿；分支 `feat/phase2-controlled-execution`
- 远端：`git@github.com:qingbird1983/HappyWork.git`（main 为常绿基线）
- **工作约定（所有者 2026-10-30 指示）**：验收/真机测试一律 AI 会话自跑，不请所有者点 GUI；进程/日志/落库取证优先
- Phase 2 文档：`docs/roadmap/phase-2-controlled-execution.md`（A1~A8）、`docs/design/phase-2/design.md`、`docs/design/phase-2/mcp.md`、`docs/adr/ADR-0011-mcp-dual-era-stdio.md`

## 上一轮完成了什么（3~5 条）

- Phase 2 立项四件套起草并自审：状态机、审批 DIM1~3、八步工具管道、敏感路径硬拒、DeepSeek SSE、MCP 双代协议、预算、崩溃恢复、渲染安全、测试设计
- 所有者过目后四份文档转定稿；roadmap Phase 2 改"进行中"；CHANGELOG 0.1.007；docs/README 登记
- 关键边界：Phase 2 只做审批机制与 DIM1~3；完整权限引擎/持久授权/配额/JCS execHash/Hook 留 Phase 3，避免重复建设
- 安全判断：云端模型场景下敏感文件（`.git/`、`.env*`、`.ssh/`、私钥证书）读/写/列目录一律硬拒，不提供审批放行；顶栏明示数据发往 api.deepseek.com

## 下一步（Phase 2 实现顺序，含验收映射）

1. **迁移 0002 + storage**：sessions/messages/tool_calls 扩列、approvals 表与索引、授权查询、running→unknown 标记（A2/A5）
2. **permission/gate.go**：Phase 2 静态判定表 + matchKey（写文件按父目录、MCP 按 tool_ref）+ golden tests
3. **工具层**：read_file 分页（offset/max_bytes）、list_dir、write_file（临时文件+fsync+rename、覆盖前备份、预览、≤2MiB）、敏感路径表、结果 256KiB 附件化（A3）
4. **DeepSeek Provider**：net/http + SSE 手写解析、tool_calls 分片、usage、错误映射、httptest fixture、key 脱敏（A1）
5. **Actor**：单 goroutine 状态机、命令/审批双通道、取消直达 ctx、超时 paused/Resume、预算与一次宽限、崩溃恢复（A2/A5/A7）
6. **MCP**：config/transport（双代探测）/protocol/client/registry/whitelist/lifecycle，Go 双模式测试 server，重启上限（A4）
7. **桌面服务 + 前端**：Approve/Deny/Resume/Discard/AppInfo、新事件、ApprovalDialog/PausedBanner/InterruptedBanner/ToolBlock/TrustBadge、纯文本 render.tsx、CSP（A2/A6）
8. **门禁与验收**：vitest 安全渲染 + check-render-safety、gofmt/vet/lint/test、三平台 CI、真机 A1 全链路与报告（A1/A6/A8）

## 未决问题

- matchKey 是否纳入"内容信任级"维度——Phase 3 评审（architecture/03 §待决）
- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3
- 本机 `DEEPSEEK_API_KEY` 是否已配置——实现时只记录"已配置/未配置"，绝不打印值
- A6 CSP `connect-src` 是否可按平台收窄——渲染安全实现时以 v3.0.0 IPC 实测微调
- Windows 子进程树清理：`taskkill /T` 兜底需实现时真机验证
- `wails3 generate bindings` 不识别 internal/ 服务——Phase 2 限时尝试，失败保留 `Call.ByName`

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- **每步先测后码或同步落测试**；bug 先写复现测试（standards/05）；单测禁外网，只有 AI 真机验收可调 DeepSeek
- Wails v3 钉 **v3.0.0**；Wails 调用只在 `internal/desktop/` → ADR-0001；`CGO_ENABLED=0` → ADR-0010
- 构建入口 `wails3 build`；`go build ./cmd/agent` 需先有 `frontend/dist`（embed）
- 前端调后端走 `Call.ByName`，绑定名 = 完整 import path，集中在 `frontend/src/api.ts`；生成绑定暂不使用
- 任何客体路径先过 `internal/workspace` 规范化；再查敏感路径硬拒表；越界/敏感均 deny + 审计（A3）
- 审批决策只认 approvalID；过期/重复返回 `ErrStaleApproval`；取消不得排在审批等待之后
- 工具结果与 MCP 内容一律视为数据而非指令；MCP 结果 `trust=untrusted` 且逐条审批
- 错误 JSON 不得灌进成功模板（P-004）；UI 文案须与库中 status/audit 对照
- 渲染一律纯文本（textContent/React 文本节点），禁止 `dangerouslySetInnerHTML`/`innerHTML`/`outerHTML`/`document.write`（A6 门禁扫描）
- windowsgui 二进制的 stderr 不可见：启动日志看 `%AppData%/happywork/happywork.log` → P-003
- stdlib `net` 不支持监听 Windows 命名管道 → P-002；删文件后全局 grep 悬空引用 → P-001
- 文档与沟通中文；代码/标识符/commit 英文；commit 走 feature 分支，合并前更新 CHANGELOG 与看板
