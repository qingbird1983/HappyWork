# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 2 实现完成 + 真机自验，待签收）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1（最小骨架）✅ 完成（2026-10-30，tag `phase-1.end`）**
- **Phase 2（受控执行闭环）验收中**：实现与本地门禁完成，A1~A7 真机通过，A8 差三平台 CI；等待所有者签收后提交/推送。报告 `docs/design/phase-2/report.md`（待签收）。
- 分支：`feat/phase2-controlled-execution`（实现尚未 commit；立项文档提交 `7315a33`）
- 远端：`git@github.com:qingbird1983/HappyWork.git`（main 为常绿基线）
- 工作约定：验收/真机测试一律 AI 自跑，不请所有者点 GUI；证据优先（截图/DB/日志/门禁）

## 上一轮完成了什么（3~5 条）

- 实现全部 Phase 2 能力：DeepSeek 流式 + function calling + usage、Actor 状态机、审批 DIM1~3、本地三工具、MCP stdio 双代、预算五上限 + 宽限、崩溃恢复、纯文本渲染 + CSP、桌面服务与前端。
- 真机验收：DeepSeek 审批写文件成功（$0.0002247/1596 tokens）；MCP 发现/调用 + untrusted + 注入按文本显示；审批中强杀 → 重启继续 → 注入"结果未知"；费用合计约 $0.0009。
- 门禁：gofmt/vet/golangci(0)/go test(13 包)/check-size/render-safety + 前端 lint/typecheck/format/test(10)/build + Windows `wails3 build` 全绿。
- 修复并沉淀 P-005~P-008；补预算五上限与 `running_tool` 崩溃恢复测试；测试文件按 600 行预算拆分。
- 文档：report.md、CHANGELOG 0.1.008、roadmap 状态、docs/README、pitfalls、本卡与 H-003。

## 下一步（按优先级，注明阻塞项）

1. **阻塞：所有者签收实现与报告**（签收后才可 commit/push/tag，见 H-003）。
2. 提交实现并推送 `feat/phase2-controlled-execution` → 盯三平台 CI（A8 最后一格）；失败当轮修复。
3. CI 绿后：报告转定稿、roadmap 改"✅ 完成 + tag `phase-2.end`"、合并 main、CHANGELOG 补记。
4. 可选：GUI 版 `running_tool` 强杀补跑（当前由 `TestCrashDuringRunningToolMarksUnknownThenResume` 覆盖同一启动恢复路径）。
5. Phase 3 立项前评审：完整权限引擎、JCS execHash、DIM4、Hook/TOCTOU（见 roadmap 与 architecture/03）。

## 未决问题

- 生成式绑定：`wails3 generate bindings` 仍 `0 Services`，继续 `Call.ByName`（已记报告偏差）。
- matchKey 是否纳入"内容信任级"维度——Phase 3 评审（architecture/03 §待决）
- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3
- A6 CSP `connect-src` 是否可按平台收窄——待 Wails IPC 演进
- MCP 远程传输、resources/prompts——Backlog

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- **每步先测后码或同步落测试**；bug 先写复现测试（standards/05）；自动化测试/CI 禁外网，只有 manual 标签真机测试可调 DeepSeek。
- **审批快照只在消费决策时比较一次**（P-006）；过期/重复 approvalID 返回 `ErrStaleApproval`。
- **终态事件发出前先清 busy**（P-005），否则完成后立即 Send 会被误拒。
- **崩溃恢复只标活动状态**；运行中工具记 `unknown`，向模型说"结果未知"而非失败（P-008）。
- **Windows 拉起控制台子进程必须 HideWindow**；强杀才用 `taskkill /T /F`（P-007）。
- 渲染一律纯文本（React 文本节点），禁 `dangerouslySetInnerHTML`/`innerHTML`/`outerHTML`/`document.write`；门禁 `scripts/check-render-safety.sh`。
- 敏感文件（`.git/`、`.env*` 除示例、`.ssh/`、私钥证书）读/写/列一律硬拒 + 审计，不提供审批放行。
- MCP 结果 `trust=untrusted`、逐条审批、无 session 作用域；MCP 环境只注声明变量，不透传 key。
- 工具结果 >256 KiB 附件化；写文件 ≤2 MiB、原子写、覆盖前单文件副本。
- windowsgui 二进制 stderr 不可见：启动日志看 `%AppData%/happywork/happywork.log`（P-003）。
- Wails 钉 v3.0.0；前端调后端 `Call.ByName`，集中在 `frontend/src/api.ts`；构建入口 `wails3 build`。
- 密钥只从 `DEEPSEEK_API_KEY` 读取，永不入库/日志/transcript；仓库禁止个人绝对路径。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
