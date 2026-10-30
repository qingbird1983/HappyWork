# H-006-Phase 3 实现、验收与收尾

- 日期：2026-10-30
- 主题：权限引擎 + Hook + execHash/TOCTOU 落地，真机验收与阶段关闭

## 本轮做了什么

- 迁移 0003：`sessions.preset`、`approvals.exec_hash/content_trust`、`tool_calls.exec_hash`、`permission_rules`；含 Phase 1/2 升级与幂等测试。
- `internal/jcs`：I-JSON 前置校验（UTF-8、重复键、整数 ≤2^53）、RFC 8785 包装、hash、官方向量/fuzz。
- `internal/permission`：映射表行、预设（内置三 + `presets.d` 严格且只收紧）、条件（路径/时间/域名）、DIM4 滑动窗口、持久规则命中/作用域/信任降级。
- `internal/hook`：五点、优先级/名称排序、超时、panic 恢复、warn/deny/fail_closed、after 只读、before_tool 白名单改参、严格 `hooks.d` 加载与指纹、secret-scan。
- Agent：Hook→规范化→JCS hash→引擎→审批（once/session/persistent + 短语）→执行前重算与路径复核→执行→after；`session:permission` 事件；规则命中审计。
- 桌面/前端：预设选择、规则面板、持久短语输入、信任/限额警示、权限徽章；AppInfo 增加 Hook/预设错误与指纹。
- 真机验收（隔离 APPDATA + DeepSeek）与报告；三平台 CI run 41066195833 全绿；合并 main、tag `phase-3.end`。

## 验证

- `go test ./...`、`go vet`、`golangci-lint`（0 issues）、`check-size`、`check-render-safety` 全绿；前端 lint/typecheck/format/14 vitest/build 全绿；Windows `wails3 build` 通过。
- 真机：写文件审批三选项、session 授权写入成功、`exec_hash` 64 hex、`content_trust=trusted`、Hook 指纹 `d94cbfc8…` 上屏且与日志一致、docs-only 自定义预设落库。

## 遗留与下一步

- Phase 4 立项：不可变审计/hash-chain、正式回滚、审计持久化；需所有者放行。
- 工具决策徽章仅在运行期展示（transcript 重载丢失）；持久/secret-scan 的 GUI 提交受宿主分类器限制，由集成测试覆盖。
- 生成式绑定仍不可用，保留集中式 `Call.ByName`。

## 已提取的沉淀

- P-009 纯 tool_calls 回合必须落 assistant 消息。
- P-010 预设收紧作用域后不得再提供持久选项。
- 复用 P-005~P-008。
