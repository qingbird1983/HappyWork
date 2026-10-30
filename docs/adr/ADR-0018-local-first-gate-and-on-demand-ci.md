# ADR-0018：门禁以本地同源脚本为主，远端 CI 按需触发；发布 dry-run 首发平台优先

- 状态：定稿（2026-10-31，所有者指示"改机制"）
- 日期：2026-10-31
- 关联：[standards/04](../standards/04-coding.md) §1、[standards/05](../standards/05-testing.md)；[ADR-0017](ADR-0017-release-signing.md)；[release-signing.md](../design/phase-4/release-signing.md)；[phase-4-audit-rollback-signing.md](../roadmap/phase-4-audit-rollback-signing.md) A6/A8；`scripts/gate.sh`

## 背景

Phase 4 把发布流水线接上 GitHub Actions 后，实际用量暴露了两个问题：

1. **额度是真实约束**。Actions 分钟按 runner 类型加权计费（Linux 1× / Windows 2× / macOS 10×）。2026-10-30 一天里 7 次运行折合约 **319 分钟额度**，其中一次 Release dry-run 就占 153 分钟。
2. **失败方式可以是"静默挂死"**。run `41102612818` 的 windows job 在"准备签名证书"步骤上没有输出地卡了 63 分钟才被平台掐掉——因为该步骤要把自签证书加进 `CurrentUser\Root`，无人值守时会弹出系统确认框，没人点。由于当时**没有任何 job 设 `timeout-minutes`**，这一件事就烧掉约 133 分钟额度。
3. 与所有者确认的产品事实是：**首发平台只有 Windows**，macOS/Linux 的验收标准是"可构建、可签名、可验签"，不承诺首发打磨度；而开发机本身就是 Windows，`go test`、`wails3 build`、签名与验签在本地都能跑。每轮推送都在远端重复三平台，验证价值低、成本高。

## 决策

1. **门禁的唯一事实来源是 `scripts/gate.sh`**，本地和 CI 跑同一份脚本。脚本覆盖：`gofmt`（去 CR 的 CI 等价比较）→ `go build` → `go vet` → `golangci-lint`（可用 `--skip-lint` 让 CI 的 action 代跑）→ `go test`（`--race` 可选）→ 前端 `format:check/lint/typecheck/test:unit` → `check-render-safety.sh` → `check-size`；`--build` 追加当前平台 `wails3 build`。
2. **日常验收在本地**：提交/推送前 `bash scripts/gate.sh --race`；不再要求"每轮推送都有远端 CI 绿"。
3. **`ci.yml` 取消 push 触发**，只保留 `workflow_dispatch`（可选平台矩阵：默认 `windows`，可加 `linux`/`macos`）与指向 main 的 `pull_request`。远端 CI 的定位改为**里程碑复核**（准备合并 main、准备发版前各跑一次）。
4. **每个 job 必须有 `timeout-minutes`**（`ci.yml` plan 5 / check 20 / build 20；`release.yml` prepare 20 / windows 25 / macos 30 / linux 25 / release 15）。两个 workflow 都加 `concurrency` + `cancel-in-progress`，同一 ref 重复触发时自动取消旧 run。
5. **发布 dry-run 默认只跑 Windows**：`workflow_dispatch` 增加 `platforms` 选择（默认 `windows`），`macos`/`linux` job 用 `if: contains(needs.prepare.outputs.platforms, …)` 门控。`v*` 正式标签仍然三平台齐跑且缺任一个即失败（沿用 ADR-0017 第 5 条），本 ADR 不放宽正式发布标准。
6. **Windows dry-run 不再触碰任何证书存储**：自签证书在内存里用 `CertificateRequest` 生成后导出 PFX；验签在 dry-run 路径改用 `Get-AuthenticodeSignature`，断言"签名存在 + 签名者就是这张临时证书 + 具备代码签名 EKU（按 OID `1.3.6.1.5.5.7.3.3` 判断，不用本地化的 FriendlyName）+ 状态只能是 `Valid`/`NotTrusted`"。`signtool verify /pa /all` 只在真实证书路径执行。理由见 P-016。
7. **A6 的取证范围随之收窄**：Phase 4 的 A6 = Windows dry-run 三件事（构建、签名、验签）跑绿；macOS/Linux 的 dry-run 留到平台范围扩大或临发版里程碑再做，报告里如实标注"未执行（按所有者决定延后）"，不写成通过。

## 后果

**正面**

- 额度消耗从"每轮推送 15–45 分钟"降到本地 0 分钟；远端只在里程碑跑，且单平台 dry-run 一次约 3–8 分钟额度。
- 卡死步骤不再吞额度：超时上限 + 无证书存储弹窗的路径，让失败在几十秒到几分钟内显式暴露。
- 本地与远端命令完全同源，减少"CI 里那套和本地不一样"的漂移。

**负面 / 需要盯住**

- 失去"每次推送都有独立机器复核"的强度：合并 main 前必须手工确认本地 `gate.sh --race` 全绿，这条写进 standards/04 的收尾清单。
- 本机没有 NSIS 与 signtool，安装包与真实链式验签只能在远端 dry-run 覆盖：`gate.sh` 的本地覆盖不等于 A6 完成，A6 仍需一次远端 Windows dry-run。
- `release.yml` 的 linux job 已知在当前 `ubuntu-latest`（Ubuntu 26.04）上失败：`E: Unable to locate package dpkg-sig`。按本 ADR 它默认不再被触发，缺陷保留在 backlog，等平台范围扩大时连同签名方式一起处理。
- dry-run 用 `NotTrusted` 作为预期结果，意味着它验证的是"签名机制可用"，不验证"公共信任链可用"；后者仍属 A7。

## 替代方案（未采纳）

- **继续每轮推送跑三平台**：最强独立复核，但一天就能吃掉月额度的 15%+，且暴露于无超时的挂死风险。
- **把自签证书装进受信任根**（原实现）：能让 dry-run 走 `signtool /pa`，但在无人值守 runner 上触发系统弹窗导致 63 分钟静默挂死，代价不可接受。
- **`ubuntu-latest` 钉回 `ubuntu-24.04`** 以保住 `dpkg-sig`：能把 Linux job 救回来，但等于为一个延后的平台范围锁定镜像版本；所有者已明确 macOS/Linux 暂不管，不值得现在改动。
