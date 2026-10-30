# 工作上下文交接卡

- 更新：2026-10-30（会话主题：架构定稿 + Phase 1 施工设计 + 最小骨架实现）

## 当前阶段与状态（对照 roadmap 看板）

- **Phase 1（最小骨架）✅ 完成（2026-10-30，tag `phase-1.end`）**：7/7 验收通过，报告见 design/phase-1/report.md
- 远端仓库已建并推送：`git@github.com:qingbird1983/HappyWork.git`（main + feat/phase1-skeleton 已合回 main）
- **工作约定（所有者 2026-10-30 指示）**：验收/真机测试一律 AI 会话自跑，不请所有者点 GUI；进程/日志/落库取证优先
- architecture/01~04、roadmap/00、phase-1 合同、design/phase-1/{design,report}.md 均**定稿**
- 下一阶段 **Phase 2（受控执行闭环）尚未立项**——立项须先写阶段文档（roadmap §2 流程）

## 上一轮完成了什么（3~5 条）

- 定稿：四篇架构 + roadmap + phase-1 合同转定稿；施工设计稿 `docs/design/phase-1/design.md`；ADR-0010（SQLite 驱动 modernc.org/sqlite，纯 Go 免 CGO）
- 骨架：cmd/agent + internal/{desktop,agent,tool,workspace,storage,model/mock,singleinst} + migrations + frontend（React19+TS+Vite 灰壳，CSP + textContent 渲染）
- 门禁：gofmt/vet/golangci-lint/go test 六包全绿；前端 prettier/eslint/tsc/vite build 全绿；`wails3 build` 产出 `bin/happywork.exe`（flag 冒烟通过）
- mock Provider：内嵌剧本 greet/read_file/tool_result/long/boom，关键词选择；40ms delta 批量；工作区越界拒绝写 audit（A5 逻辑有测试覆盖）
- 单实例锁：flock + focus.req 轮询（stdlib 不支持 Windows 命名管道监听 → P-002，architecture/04 与设计稿已同步）

## 下一步（按优先级，注明阻塞项）

1. **Phase 2 立项**：写 `docs/roadmap/phase-2-*.md` 范围合同 + `docs/design/phase-2/` 施工设计稿（DeepSeek Provider 接入——届时核对 env var 名 `DEEPSEEK_API_KEY`；Actor 完整状态机；四维审批弹窗对齐 research/03 三要素；MCP stdio；渲染安全含 A6 注入用例）
2. Phase 2 设计定稿（所有者过目）→ `feat/phase2-*` 开工
3. 遗留跟催：macOS 公证演练（所有者开发者凭据，Phase 4 前置）；`wails3 generate bindings` 不识别 internal/ 服务（Phase 2 切换生成式绑定时解决）
4. 发布工程（不急）：actions 升级消除 Node20 弃用告警；ubuntu-latest 迁移 26 的适配观察

## 未决问题

- matchKey 是否纳入"内容信任级"维度——Phase 3 评审（architecture/03 §待决）
- Hook 签名密钥签发方式（倾向本地生成 + TOFU）——Phase 3
- 本机 DeepSeek 环境变量名确认（代码默认读 `DEEPSEEK_API_KEY`，支持配置覆盖）——Phase 2 接入时核对
- A6 CSP 的 `connect-src` 清单是否可按平台收窄——Phase 2 渲染安全时核对

## 关键提醒（坑/规范变更/易错点，链接 pitfalls 编号）

- Wails v3 钉 **v3.0.0**（CLI 与库同线）；所有 Wails 调用只在 `internal/desktop/` → ADR-0001
- **CGO_ENABLED=0 是项目约束**：禁止引入 CGO 依赖（SQLite 用 modernc 纯 Go）→ ADR-0010
- 构建入口 `wails3 build`（Taskfile 链）；`go build ./cmd/agent` 需先有 `frontend/dist`（embed；CI 里 wails3 build 自带前端步骤）
- 前端调后端走 `Call.ByName`，绑定名 = **完整 import path** `.Struct.Method`（非短包名，真机踩过），全部集中在 `frontend/src/api.ts`；生成绑定（frontend/bindings/，已 gitignore）暂不使用
- mock 剧本选择：上一条是工具结果 → 固定 `tool_result` 剧本，否则按关键词（boom/long/read_file·读取/缺省 greet）——改剧本别破坏这个防死循环分支
- 任何客体路径先过 `internal/workspace` 规范化管线；UNC/8.3/盘符相对路径直接拒绝 → ADR-0004
- 权限判定只认工具映射表（Phase 3 接管，Phase 1 是硬编码边界检查）→ ADR-0002/0003
- stdlib `net` 不支持监听 Windows 命名管道（"pipe"=内存管道）→ P-002
- windowsgui 二进制的 stderr 不可见：启动日志双写 `%AppData%/happywork/happywork.log`，双击无反应先看这个文件 → P-003
- vite build 会清空 dist（连同占位 .gitkeep）：vite.config 已设 `emptyOutDir: false` 保 CI embed 可编译；旧哈希产物会累积，Phase 1 量级可接受
- 删模板/占位文件后全局 grep 悬空引用 → P-001
- ZCode 参照只读信息结构不搬代码（ADR-0009）；DeepSeek key 只从环境变量读不入库（ADR-0008、standards/06）
- 前端灰盒推进：信息完整性不降级，视觉零投入 → roadmap Backlog
