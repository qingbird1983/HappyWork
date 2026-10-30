# 工作上下文交接卡

- 更新：2026-10-30（会话主题：Phase 3 立项放行，权限引擎与 Hook 开工）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1 ✅ 完成（tag `phase-1.end`）**
- **Phase 2 ✅ 完成（tag `phase-2.end`）**：8/8 验收，报告定稿，main CI 绿。
- **Phase 3（权限引擎与 Hook）进行中（2026-10-30 立项）**：范围合同、设计、Hook 设计、ADR-0012/0013/0014 已定稿；6 项决策已放行；依赖已登记。
- 分支：`feat/phase3-permission-engine`；main 保持 phase-2.end 基线。
- 远端：`git@github.com:qingbird1983/HappyWork.git`（main 常绿）
- 工作约定：验收/真机测试 AI 自跑；所有者签收后才 commit/push/tag；文档先行已满足。

## 上一轮完成了什么（3~5 条）

- 起草并放行 Phase 3：完整权限引擎（映射表/预设/持久规则/DIM4）、内容信任级、JCS execHash/TOCTOU、编译期内置 Hook、迁移 0003、前端最小界面。
- 6 项决策：JCS 用 RFC 8785 参考实现；Hook 仅编译期内置、签名延后；内容信任级为条件不并入 matchKey；持久规则仅可逆写；三预设 + 只收紧自定义；限额只约束自动放行、工作区外读取仍硬拒。
- 依赖登记：JCS（Apache-2.0，固定伪版本）、yaml.v3（MIT/Apache-2.0）；文档状态与看板已更新。
- Phase 2 收尾：合并 main、tag `phase-2.end`、三平台 CI 全绿（run 41054482312/41055399869）。

## 下一步（Phase 3 实现顺序，含验收映射）

1. **迁移 0003 + storage**：`sessions.preset`、`approvals.exec_hash/content_trust`、`tool_calls.exec_hash`、`permission_rules` CRUD 与升级测试（A3/A4）。
2. **internal/jcs**：I-JSON 校验、JCS 包装、hash、官方向量/fuzz（A4）。
3. **permission 引擎**：映射表注册校验、预设加载/只收紧、条件、最严合并、可逆修正、限额（A1/A2/A6）。
4. **持久规则 + 审批扩展**：短语校验、命中/撤销、信任闩锁与作用域降级（A3/A7）。
5. **hook 框架**：接口、runner（顺序/超时/panic/失败策略）、严格配置、指纹、secret-scan（A5）。
6. **agent 管道**：Hook→规范化→JCS hash→引擎→审批→执行前校验→执行→after；`session:permission` 事件（A4/A7）。
7. **桌面 + 前端**：预设选择、规则面板、审批持久短语/信任警示、限额提示（A2/A3/A7）。
8. **门禁与验收**：golden/fuzz/TOCTOU/hook/quota/迁移测试、前端五连、三平台 CI、真机验收与报告（A8）。

## 未决问题

- JCS 上游无 v1 tag：固定伪版本；停止维护时按 ADR-0012 迁移。
- 配额内存计数重启归零：Phase 4 评估持久化。
- 路径二次校验与执行之间仍有残窗：沙箱前承认。
- 外部进程 Hook/签名：独立 ADR（ADR-0013 已限定本期）。

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- 审批快照只在消费决策时比较一次（P-006）；终态先清 busy（P-005）。
- 崩溃恢复只标活动状态；运行中工具记 unknown（P-008）。
- Windows 控制台子进程必须 HideWindow；强杀才 taskkill /T /F（P-007）。
- 敏感文件硬拒不提供审批放行；密钥只走环境变量，不入库/日志/transcript。
- 渲染一律纯文本；`scripts/check-render-safety.sh` 不得放宽。
- 自动化测试/CI 禁外网；只有 manual 标签真机测试可调 DeepSeek。
- JCS 前必须拒绝非 I-JSON 数字；Hook 改参仅 before_tool 白名单，after 只读。
- 持久规则不覆盖 deny/不可逆；不可信内容在场时不命中 session/persistent。
- 文档与沟通中文；代码/标识符/commit 英文；feature 分支 + 签收后推送。
