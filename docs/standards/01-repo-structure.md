# 规范 01：仓库结构与命名

- 状态：定稿（2026-10-30 按本项目回填 §2 代码布局与 §3 命名）
- 日期：2026-10-30

## 1. 目录布局（治理部分，所有项目一致）

```
/                     仓库根：README.md（英）/ README.zh-CN.md（中）/ AGENTS.md / CHANGELOG.md / LICENSE / .gitignore / .gitattributes / .editorconfig
.agents/               AI 开发知识库与交接中枢（规范见其 README；预算受 standards/08 约束）
.agents/skills/       可复用方法沉淀（S-NNN）+ vendored 第三方技能子目录（注明来源与许可证，不计预算）
briefs/                所有者本地输入区（参考/建议/简报；gitignore 整目录永不入库；规范见其 README）
docs/                 全部文档（唯一事实来源；结构见 docs/README.md）
scripts/              构建/检查脚本（CI 用，如 check-size）
.github/workflows/    CI 流水线
cmd/ internal/ frontend/ migrations/   代码目录（Go + Wails 布局，见 §2）
```

- 空目录不进版本控制——目录随首批内容建立；`fixtures/`、`examples/` 等占位目录不用即删（本项目初始化时已删，启用时重建）。

## 2. 代码目录预设（初始化时三选一或自定义，回填到本节）

> 自定义布局同样合法，但一经回填即为本规范内容，变更走规范修订（standards/09）。

**预设 A —— 单包项目**（脚本 / 小应用 / 单库）：

```
src/          全部源码
tests/        集成测试（单元测试随源码）
```

**预设 B —— 单仓多模块**（monorepo：一个仓多种语言或多个包）：

```
packages/     各库/模块（每包自带清单与测试）
apps/         可执行应用（薄壳，业务逻辑下沉 packages/）
```

**预设 C —— 内核 + 壳**（一个核心库 + 多个入口）：

```
core/ 或 libs/    核心库（零 UI、零入口依赖）
apps/             各入口（CLI / 桌面 / Web，只做薄壳）
```

本项目选定：**自定义（Go + Wails v3 单应用布局，与 [architecture/01](../architecture/01-overview.md) §代码目录骨架一致）**

```
cmd/agent/          可执行入口（main.go 只装配）
internal/           全部内部包：agent / model / tool / permission / sandbox /
                    rollback / audit / mcp / hook / storage / secrets / desktop / singleinst
frontend/           WebView 前端源码（TS；框架 Phase 1 前定）
migrations/         SQLite 迁移脚本
fixtures/           测试夹具（Phase 1 起随首批夹具重建）
```

- 依据：Go 生态惯例（`cmd/` + `internal/`）与 Wails 项目结构（`frontend/`）的结合；架构分包与依赖方向见 [architecture/01](../architecture/01-overview.md)。

## 3. 命名约定

| 对象 | 规则 | 示例 |
|---|---|---|
| 文档文件 | 两位序号 + kebab-case（adr 用 `ADR-NNNN` 前缀，roadmap 阶段用 `phase-N-` 前缀） | `03-critical-seams.md` |
| 源码文件/模块 | Go 源文件 snake_case（`tool_registry_test.go`）；前端 TS/TSX camelCase 标识符、kebab-case 文件名，全仓一致 | `actor.go`、`session-writer.ts` |
| 目录 | Go 强约定从惯例：包目录用小写单词（`permission`）；前端与文档目录 kebab-case | `internal/permission` |
| 分支 | `feat\|fix\|docs\|refactor\|test\|chore/<短横线描述>` | `feat/session-writer` |
| tag | 阶段：`phase-N.start` / `phase-N.end`（annotated）；发布：`vX.Y.Z` | `phase-1.end` |

## 4. 内容纪律

- 仓库内**禁止**出现：密钥/凭证、个人绝对路径（如 `C:\Users\<名>`）——文档引用外部项目用仓库相对描述或 URL（standards/06）。
- 根目录只放本规范 §1 列出的文件；新增根文件须先修订本规范。
- 根 `README.md`（英）与 `README.zh-CN.md`（中）：**只做项目说明与导航**，两份内容保持一致、头部互挂语言切换链接；禁止放进度看板、状态或流水账（看板唯一在 `docs/roadmap/00-roadmap.md`，流水唯一在 CHANGELOG）。
- 根 `AGENTS.md`：**薄索引入口**（导航 + 铁律链接），禁止记录状态/进度/阶段指针；AI 的"当前状态"唯一来源是 `.agents/CONTEXT.md`。
- 构建产物目录一律不入库（名称在 `.gitignore` 回填）。
