# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 2 验收通过并合并，准备 Phase 3）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1（最小骨架）✅ 完成（2026-10-30，tag `phase-1.end`）**
- **Phase 2（受控执行闭环）✅ 完成（2026-10-30，tag `phase-2.end`）**：8/8 验收通过，A1/A8 两项一票否决均过；报告 `docs/design/phase-2/report.md`（定稿）；CI run 41054482312 全绿。
- **Phase 3（权限引擎 + Hook + TOCTOU）未开始**：立项前先读 roadmap §3 与 architecture/03 的待决项。
- 分支：`feat/phase2-controlled-execution` 已合并 main（merge commit 带分支语义；tag 指向合并提交）
- 远端：`git@github.com:qingbird1983/HappyWork.git`（main 为常绿基线）
- 工作约定：验收/真机测试一律 AI 自跑，不请所有者点 GUI；证据优先（截图/DB/日志/门禁）

## 上一轮完成了什么（3~5 条）

- Phase 2 全量实现：DeepSeek 流式 + function calling + usage、Actor 状态机、审批 DIM1~3、本地三工具、MCP stdio 双代、预算五上限 + 宽限、崩溃恢复、纯文本渲染 + CSP、桌面服务与前端。
- 所有者签收后提交（`f474955`）、推送并跑三平台 CI：check + windows/macos/ubuntu build 全绿。
- 真机验收：DeepSeek 审批写文件成功；MCP 发现/调用 + untrusted + 注入按文本显示；审批中强杀 → 重启继续 → 注入"结果未知"；费用合计约 $0.0009。
- 修复并沉淀 P-005~P-008；补预算五上限与 `running_tool` 崩溃恢复测试；测试文件按 600 行预算拆分。
- 文档：报告定稿、CHANGELOG 0.1.008/0.1.009、roadmap 改完成、docs/README、pitfalls、H-003/H-004、本卡。

## 下一步（按优先级，注明阻塞项）

1. **Phase 3 立项**：起草范围合同 + 施工设计 + ADR（权限映射表 golden tests、JCS execHash、DIM4 限额、persistent 规则、进程内 Hook、TOCTOU 管道），所有者过目后转定稿。
2. Phase 3 开工前评估 Phase 2 遗留：生成式绑定（`wails3 generate bindings` 仍 0 services）、GUI 版 `running_tool` 强杀补跑、matchKey 是否纳入内容信任级。
3. 后续阶段：Phase 4 不可变审计/回滚/签名；Phase 5 OS 沙箱；UI/UX 精修（触发条件：Phase 2 验收通过，已满足，可评估排期）。
4. Backlog 评审：远程 MCP、多会话并行、Markdown 渲染等（roadmap §3）。

## 未决问题

- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3
- JCS execHash 的字段范围与向后兼容——Phase 3
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
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 所有者签收后推送。
