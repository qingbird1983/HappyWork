# H-003 Phase 2 受控执行闭环实现与自验

- 日期：2026-10-30
- 主题：Phase 2 全量实现、真机验收 A1~A7、本地门禁全绿，等待签收与推送

## 本轮做了什么

- 完成 Phase 2 全部实现：迁移 0002 + approvals、permission 门卫、read_file 分页 / list_dir / write_file（原子写 + 备份）、DeepSeek SSE + function calling + usage、Actor 完整状态机、审批 DIM1~3、MCP stdio 双代协议、预算五上限 + 一次宽限、崩溃恢复、桌面服务与前端（审批弹窗、暂停/中断横幅、工具块、纯文本渲染、CSP）。
- 真机验收（隔离 `APPDATA` + 截图 + UIAutomation）：
  - DeepSeek 会话写入 `hello.txt` 成功，审批行与 usage/费用入库，费用约 $0.0004；
  - Python MCP server 被发现并调用，来源标记不可信、逐条审批，注入 `<script>` 按纯文本显示；
  - 审批等待中强杀 → 重启 interrupted → 继续注入"结果未知" → 完成。
- 手工真机测试 `TestDeepSeekLive`（manual 标签）通过：2 工具、1740 tokens、$0.000457，key 无泄漏。
- 验收期间修复：终态忙标志、审批快照时序、附件路径、MCP 控制台窗口、MCP matchKey、重启误标 idle、taskkill/stderr/MCP 失败提示/暂停文案、空数据目录保护。
- 补测试：预算五条上限、`running_tool` 强杀恢复、恢复中间态断言去脆化；测试文件按体量拆分。
- 文档：`design/phase-2/report.md`（待签收）、CHANGELOG 0.1.008、roadmap 状态、docs/README 登记。

## 验证

- `gofmt`（本阶段文件）/ `go vet ./...` / `golangci-lint run ./...`（0 issues）/ `go test ./...`（13 包）全通过。
- `scripts/check-size`、`scripts/check-render-safety.sh` 通过。
- 前端 `lint / typecheck / format:check / test:unit`（10/10）/ build 通过；Windows `wails3 build` 通过。
- 真机证据、隔离数据目录与脚本在本地 `.task/phase2-acceptance/`（不入库）。

## 遗留与下一步

1. 所有者签收 `docs/design/phase-2/report.md` 后：提交实现、推送 `feat/phase2-controlled-execution`，盯三平台 CI 补绿（A8 最后一格）。
2. CI 绿后更新报告状态为定稿、roadmap 改"✅ 完成（日期 + tag）"、打 `phase-2.end`、合并 main。
3. 可选补跑：GUI 版 `running_tool` 强杀（当前由集成测试覆盖同一恢复路径）。
4. 生成式绑定仍只产出事件，保留 `Call.ByName`（已写入报告偏差表）。

## 已提取的沉淀

- P-005 终态未清 busy 标志导致立即发送被拒
- P-006 审批快照提前清除造成假参数漂移
- P-007 windowsgui 子进程弹出控制台窗口
- P-008 崩溃恢复只标活动会话，运行中工具记 unknown
