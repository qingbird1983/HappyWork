# 更新日志（CHANGELOG）

本文件是**面向非技术读者的进展流水**，每次文档或阶段变更都必须在此登记一条（格式规范见 `docs/standards/03-documentation.md`）。技术读者另见 git 历史。

格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；版本号同时用于标记「文档/阶段里程碑」，与代码发布版本共用一个序列。

---

## [0.1.010] - 2026-10-30

### Phase 3「权限引擎与 Hook」立项放行

- 所有者过目并放行 6 项决策：JCS 采用 RFC 8785 参考实现；Hook 仅编译期内置、签名/TOFU 延后；内容信任级作为条件、不并入 matchKey；持久规则仅限可逆写；内置三预设 + 只收紧自定义；限额只约束自动放行、工作区外读取仍硬拒、删除/shell/网络继续不做。
- Phase 3 范围合同、施工设计、Hook 设计与 ADR-0012/0013/0014 全部转定稿；看板改"进行中"。
- 新依赖已登记 standards/04：JCS 参考实现（Apache-2.0，固定伪版本）与 YAML 解析（MIT/Apache-2.0）。分支 `feat/phase3-permission-engine` 开工。

---

## [0.1.009] - 2026-10-30

### Phase 2「受控执行闭环」验收通过（tag phase-2.end）

- 所有者签收实现与验收报告：8/8 通过，A1（真实 DeepSeek 闭环）与 A8（本地/CI/三平台构建）两项一票否决均过；真机验收费用约 $0.0009。
- 三平台 CI run 41054482312 全绿（check + windows/macos/ubuntu build）；分支合并 main 并打 annotated tag `phase-2.end`。
- 下一阶段：Phase 3（完整权限引擎 + 进程内 Hook + TOCTOU）；生成式绑定与 GUI 版 `running_tool` 补跑等遗留见[报告](docs/design/phase-2/report.md)偏差表。

---

## [0.1.008] - 2026-10-30

### Phase 2「受控执行闭环」实现与验收

- 实现完成：DeepSeek 流式 + function calling（usage/费用入库，key 只走 `DEEPSEEK_API_KEY`）、完整会话状态机（审批/暂停/保存/中断）、写文件/列目录/分页读、单次与本会话审批、敏感文件硬拒、MCP stdio 双代协议与逐条审批、预算五条上限 + 一次宽限、崩溃恢复、纯文本渲染与 CSP 加固。
- 真机验收 A1~A7 通过：真实模型完成"读 → 列目录 → 审批写文件"闭环；MCP 工具被发现并调用，注入内容按纯文本显示；审批弹窗期间强杀后可从"上次中断"继续。验收费用约 $0.0009（[报告](docs/design/phase-2/report.md)）。
- A8 本地门禁全绿（fmt/vet/lint/test/check-size/render-safety + 前端五连 + Windows 构建）；三平台 CI run 41054482312 全绿。
- 真机与测试期间修复 8 类缺陷（终态忙标志、审批快照时序、附件路径、MCP 控制台窗口、matchKey 落库、重启误标空闲会话、子进程清理/日志上限、MCP 失败提示），并补预算与 running_tool 崩溃恢复测试。
- 遗留：生成式绑定仍只产出事件（保留 `Call.ByName`）；`running_tool` 强杀由集成测试覆盖，真机强杀点为审批等待态。

---

## [0.1.007] - 2026-10-30

### Phase 2「受控执行闭环」立项

- 所有者放行：Phase 2 范围合同、施工设计、MCP 设计与 ADR-0011 全部转定稿。
- 本期要做的：接入 DeepSeek 真实模型（密钥只走环境变量、不入库不打印）；补齐会话状态机与暂停/恢复；写文件、目录浏览与"单次/本会话"审批弹窗；接入 MCP 工具；预算刹车（500 万 token / 2 美元等）；渲染安全加固。
- 明确不做：完整权限引擎、持久授权、参数哈希锁定、Hook、正式回滚、删除/命令执行、沙箱、Markdown 渲染（留待后续阶段）。
- 敏感文件（`.git`、`.env`、私钥证书等）一律硬性拒绝，不提供审批放行——云端模型场景下，批准执行无法撤回已发送的内容。
- 开工分支 `feat/phase2-controlled-execution`；8 项验收中"真实 DeepSeek 跑通"与"本地/CI 全绿 + 三平台构建"为一票否决。

---

## [0.1.006] - 2026-10-30

### Phase 1「最小骨架」完成（tag phase-1.end）

- 三平台构建（Windows/macOS/Linux）CI 矩阵全绿，7/7 验收场景通过（[报告](docs/design/phase-1/report.md)）；应用可建会话、mock 流式回复、读工作区文件、越界拒绝并留审计、单实例聚焦、重启恢复。
- 真机验收期间修复四类缺陷（GUI 启动静默失败、IPC 绑定名、工具失败被渲染成假成功等），全部沉淀 pitfalls（P-002~004）。
- 遗留：macOS 公证演练（待开发者凭据，Phase 4 前置）；A6 注入手工用例随 Phase 2 渲染安全补做。

---

## [0.1.005] - 2026-10-30

### 架构基线转定稿，Phase 1 开工（ADR-0010）

- 所有者放行：architecture/01~04 与 roadmap 转定稿；Phase 1 施工设计稿定稿（docs/design/phase-1/design.md：模块切片、事件契约、数据模型、mock Provider 剧本、依赖清单）。
- SQLite 驱动选型定为 modernc.org/sqlite（纯 Go 免 CGO，保三平台构建）；前端依赖清单同步登记 standards/04 附录 A。
- Phase 1 在 feat/phase1-skeleton 分支开工：Wails 壳 + SQLite + mock 模型 + read_file + 单实例锁。

---

## [0.1.004] - 2026-10-30

### 真机测试与 UI 参照两项决策落盘（ADR-0008/0009）

- 开发期真机测试选用 DeepSeek 模型（官方 API 事实已调研沉淀），测试密钥走环境变量、绝不入库；所有者授权开发期间由 AI 会话执行真机测试（自动化测试仍全部打桩）。
- 前端界面信息结构确定以 ZCode 客户端为参照（只参考信息结构，不复制其代码与视觉设计）；需要细节截图时按流程向所有者索取。

---

## [0.1.003] - 2026-10-30

### UI 策略明确：前期灰盒，精修入 Backlog

- 明确各阶段前端一律"灰盒"（默认样式、零视觉投入），UI/UX 精修登记入 Backlog，触发条件为 Phase 2 验收通过且审批弹窗信息结构冻结；审批弹窗与来源标记的信息完整性属安全基线，不随灰盒降级。

---

## [0.1.002] - 2026-10-30

### 前端框架定为 React + TypeScript（ADR-0007）

- 综合项目特性（AI 接力开发、安全渲染不可信内容、桌面壳体积无关）选定 React + TypeScript，前端工具链（prettier / eslint / tsc）纳入 CI 门禁。
- README 待决策表清空；Phase 1 开工前置条件中的框架选型项已解决。

---

## [0.1.001] - 2026-10-30

### 设计基线落盘与仓库治理初始化（Pre-code）

- 完成设计方案评审并落盘 V3 架构基线（四篇：总览 / 核心循环与数据 / 权限与审批 / 安全与扩展），修订要点：权限模型合并为单一工具映射表、审批范围与执行锁定用两套指纹分离、文件路径强制规范化、跨平台（Windows）假设修正、快照降级为备份式回滚、沙箱分期落地。
- 评审依据与外部事实核实（Wails v3 已转正等）沉淀为调研文档；六项重大决策写成 ADR（含许可证选 MIT）。
- 仓库治理初始化完成：双语 README、路线图看板（Phase 0 完成，Phase 1 最小骨架未开始）、术语表、规范按 Go 项目回填、CI 门禁与体量哨兵就绪、git 启用并打 phase-0.end 标签。
- 文档同步清单：research/01；ADR-0001~0006；architecture/01~04；roadmap/00 + phase-1；standards/01/04/05 回填；看板已同步。

---

