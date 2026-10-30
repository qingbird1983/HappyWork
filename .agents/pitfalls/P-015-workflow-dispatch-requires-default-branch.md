# P-015 workflow_dispatch 只能触发默认分支上已存在的 workflow

## 现象

Phase 4 的 A6 要求"三平台签名流水线跑一次 dry-run 作为验收证据"。分支推送后 `ci.yml` 已全绿，执行：

```
gh workflow run release.yml --ref feat/phase4-audit-rollback-signing
HTTP 404: workflow release.yml not found on the default branch
```

`gh workflow list --all` 里只有 `CI  active`，`Release` 根本不出现——不是权限问题，也不是 YAML 写错，本地 lint/解析都正常。

## 根因

GitHub 只为**默认分支（main）上存在**的 workflow 文件注册可手动触发入口：`workflow_dispatch` 与 `schedule` 都受这条限制，`--ref` 只是指定运行时检出的分支，不能用来"引用一条只在 feature 分支上的 workflow"。因此新写的流水线在合入默认分支之前，既无法手动触发，也无法定时触发，只能靠它自己的 push/PR 事件（若有）验证。

本项目还叠加了第二条规则：**阶段收尾要求签收后合并 main**，而 A6 证据要求流水线已在 main 上——两者顺序互相依赖。

## 修复（流程处置，不是代码修复）

- A6 不能在同一 feature 分支内自证。可行顺序二选一，必须让所有者决定：
  1. `ci.yml` 全绿 + A5 真机签收 → 合并 main → 在 main 上 dispatch dry-run 取证 → 问题用后续 fix 分支修（dry-run 永不发布，失败无破坏性）→ 再补 `phase-4.end`；
  2. 把默认分支临时指向 feature 分支再触发（不推荐：污染协作者对 main 的语义，且 `release` job 条件依赖 `github.event_name`，切换默认分支不改变"不发布"但会改变看板事实）。
- 报告中把 A6 的状态写成"阻塞：需 workflow 先在默认分支落地"，不写成"待执行"，避免下一轮误判成我漏跑。

## 预防

- 新写发布流水线时，立项文档里就写清"A6 证据的取得时机 = 合入默认分支之后"，并把这条约束列进验收标准的依赖项。
- 遇到 `gh workflow run` 报 404 或 workflow 不出现在列表里，先查该文件在不在默认分支，再怀疑语法/权限。
- 不要把"本地 YAML 解析通过 + `ci.yml` 绿"当成流水线可用的证据：`release.yml` 从未在远端执行过一次。

##（如复发）再次发生记录

- （无）

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml`、`docs/design/phase-4/report.md` §5、`docs/design/phase-4/release-signing.md` §9。
