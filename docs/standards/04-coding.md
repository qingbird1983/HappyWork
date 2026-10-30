# 规范 04：代码规范

- 状态：定稿（2026-10-30 回填 §1 工具链与附录 A；修订走 ADR）
- 日期：2026-10-30

## 1. 工具链与硬门槛（已回填）

> 原则：**每个坑位必须有自动化工序**——格式化器管风格、linter 管坏味道、CI 管执行；没有工具支撑的规则等于没有规则。

| 坑位 | 工具 | 命令 | CI 强度 |
|---|---|---|---|
| 格式化 | gofmt（Go）/ prettier（React 前端） | `gofmt -l .`；前端 `npm run fmt:check` | check 挂门禁 |
| Lint | golangci-lint（govet、errcheck、staticcheck 等）+ eslint（React 前端） | `golangci-lint run`；前端 `npm run lint` | error 级挂门禁 |
| 类型检查 | go vet（Go 强类型编译）+ tsc（React 前端） | `go vet ./...`；前端 `npm run typecheck` | 挂门禁 |
| 构建 | `go build ./...`（CI 门禁）；`wails3 build`（本地/发布，三平台） | `go build ./...` | 挂门禁 |

- 锁文件一律提交入库；依赖版本锁定（Go：`go.mod` + `go.sum`；前端：`package-lock.json`，npm）。
- 本项目语言：**Go ≥ 1.26**（Wails v3 要求，见 [ADR-0001](../adr/ADR-0001-adopt-go-wails3.md)）+ **React + TypeScript**（[ADR-0007](../adr/ADR-0007-frontend-react.md)，Vite 构建）；Wails v3 钉 minor 版本线。
- 代码未开工（Pre-code）：上表命令在 `go.mod` / `frontend/package.json` 落地前 CI 以守卫条件跳过（见 `.github/workflows/ci.yml`）。

## 2. 错误处理

- 错误必须显式传播与分类（类型化错误 / error code），禁止裸抛/裸捕获后吞掉。
- 库层保留错误链来源；应用层给错误补充用户可读上下文后再展示。
- "不可能失败"的断言（unwrap/panic/assert）仅限测试与初始化早期；确属不变量处必须注释论证。

## 3. 并发与资源

- 共享状态优先消息/事件而非锁；必须加锁时注释注明锁序。
- 一切占用资源的对象（文件句柄/进程/网络连接）必须有确定性的释放路径；异步取消必须显式传递，禁止"吞取消"。
- 危险能力（FFI / unsafe / 反射执行 / 动态加载）默认禁用；确需先写 ADR。

## 4. 公共 API 纪律

- 对外接口（库的导出、HTTP API、CLI 参数、文件格式）100% 文档注释（首行一句话）。
- 公共 API 变更 = 语义级变更：先文档后代码；对外承诺一经发布按 semver + 弃用流程（标注弃用 + 迁移说明）。
- 内部实现默认最小可见性，扩大可见性须有理由。

## 5. 依赖准入

- 原则：少而稳。优先官方/主流维护包；同等功能不引入第二个。
- **新依赖流程**：在本篇**附录 A（依赖准入表）**登记——名称、版本、用途、为何必须、维护活跃度、备选与否决理由；**先登记后引入**。
- 禁止引入：未审计的 git 直引依赖、已撤回版本、与既有依赖功能重叠的包。
- "为了一个小功能引入一个大依赖"视为超标行为（standards/08 §5）。

## 6. 注释与可读性

- 注释写"为什么/约束"，不复述代码。
- 临时方案必须带 `TODO(ADR-xxxx)` 或 issue 引用，禁止无主 TODO。
- 测试位置按语言惯例：单元测试随源码；集成测试集中目录。

## 7. 写码纪律（ponytail，vendored 技能）

- 写任何代码前参照 `.agents/skills/ponytail/SKILL.md`（MIT，vendored，语言无关）：**最小完整改动**——只写任务必需的代码，优先复用仓库既有结构与依赖，不造没人要的抽象/包装/"为以后准备"的代码；一行能写清就不写三行；修 bug 先 grep 全部调用方、在共享代码处修根因一次。
- 但**绝不削减**：信任边界校验、防数据丢失的错误处理、安全、可访问性、用户明确要求的内容。
- 新增非平凡逻辑（分支/循环/解析器/资金与安全/全新脚本或应用）必须附带一个小测试或断言式自检；带已知局限的捷径用 `shortcut: <局限>, <何时升级>` 格式注释。
- 与本规范的关系：ponytail 是工作纪律（怎么想），本规范是硬门槛（必须过）；冲突时以本规范为准。其 `shortcut:` 标记在本项目按 §6 落地为 `TODO(...)` 形式。

## 附录 A：依赖准入表

> 规则：先登记后引入；"计划"行在首次引入时确认版本并转正。

| 依赖 | 版本线 | 用途 | 为何必须 | 维护方 | 备选与否决理由 |
|---|---|---|---|---|---|
| github.com/wailsapp/wails/v3 | v3.0 稳定线（钉 minor） | 桌面壳与 WebView 绑定（[ADR-0001](../adr/ADR-0001-adopt-go-wails3.md)） | 技术栈基石 | Wails 团队（活跃） | Electron/纯 CLI 已在 ADR-0001 否决 |
| github.com/gofrs/flock | v0.x 主线 | 单实例锁跨平台（Windows LockFileEx / POSIX flock） | 架构 04 §单实例锁指定 | gofrs 社区（活跃） | 自研锁原语：重复造轮子且 Windows 易错 |
| modernc.org/sqlite | v0.x 主线 | transcript 持久化（database/sql 接口） | 免 CGO 保三平台构建（[ADR-0010](../adr/ADR-0010-sqlite-driver.md)） | modernc.org（活跃） | mattn/go-sqlite3：CGO 工具链与 A1 三平台构建冲突（ADR-0010） |
| react / react-dom | 19.x | 前端 UI（[ADR-0007](../adr/ADR-0007-frontend-react.md)） | 前端栈基石 | Meta 社区（活跃） | Vue/Svelte 已在 ADR-0007 否决 |
| @wailsio/runtime | 3.0 线 | 前端事件订阅与服务调用 | Wails 前端运行时，与 wails/v3 配套 | Wails 团队（活跃） | 自写 IPC 封装：重复且易与后端漂移 |
| typescript / vite / @vitejs/plugin-react | 7.x / 8.x / 6.x | 前端开发工具链 | ADR-0007 门禁（tsc）与构建依赖 | 各自社区（活跃） | webpack：配置面大，灰盒期无必要 |
| eslint + typescript-eslint + eslint-plugin-react-hooks | 10.x / 8.x / 7.x | 前端静态检查 | CI 门禁（standards/01） | 各自社区（活跃） | biome：生态尚浅 |
| prettier | 3.x | 前端格式化 | CI 门禁 | prettier 社区（活跃） | dprint：小众 |
