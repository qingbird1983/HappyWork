# 规范 02：Git 工作流

- 状态：定稿（项目暂未启用 git 时，本篇在 `git init` 后生效；§4 CHANGELOG 义务对任何工作方式即期生效）
- 日期：2026-10-30

## 1. 分支模型

- `main`：常绿分支，任何时候 CI 应可通过。
- 一切变更（含文档）经 feature 分支合并入 main；禁止直接 push 到 main。
- 分支生命周期短平快：一个分支对应一个可独立描述的变更集；完成即合并，不长期滞留。

## 2. 提交信息（Conventional Commits）

- 格式：`type(scope): subject`（英文）；body 可中文，说明"为什么"。
- type：`feat` / `fix` / `docs` / `refactor` / `test` / `chore` / `build`。
- scope：受影响模块/目录名（如 `session`、`docs`、`ci`）。
- 示例：`feat(session): implement append-only entry writer`、`docs(adr): add ADR-0007 on data format`。

## 3. 合并与里程碑

- 合并方式：merge 或 squash 皆可，但 merge commit 信息须保留分支语义。
- 阶段里程碑：开工打 `phase-N.start`、验收通过打 `phase-N.end`（annotated tag，附一句话验收结论）。
- 版本号：文档阶段沿用 CHANGELOG 的里程碑语义；代码发布用 semver，`vX.Y.Z` tag。

## 4. CHANGELOG 义务

- 每次合并入 main 都必须更新 `CHANGELOG.md`：**面向非技术读者措辞**（说"能做什么/修了什么"，不说重构细节）。
- 进度类变更（阶段状态、验收结果）同日更新 roadmap 看板。

## 5. 回滚

- main 上发现问题优先"向前修复"；确需回滚时 revert（禁止 force-push 已公开历史）。
- 用户数据、本地配置不进 git，与代码回滚无关（standards/06）。
