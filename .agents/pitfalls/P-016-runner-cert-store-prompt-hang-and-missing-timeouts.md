# P-016 无人值守 runner 上导入受信任根证书会挂死；没有 timeout-minutes 让它烧掉一小时额度

## 现象

Phase 4 的 A6 dry-run（run `41102612818`）跑了 **1 小时 8 分钟**：`prepare` 1.5 分钟、`macos` 106 秒成功、`linux` 20 秒失败（`E: Unable to locate package dpkg-sig`），而 `windows` job 从 17:58 一直挂到 19:04:56 被平台掐掉，结论 `cancelled`。日志里最后一条正常输出停在 18:01:24 的步骤 **准备签名证书**，之后 63 分钟**一个字符都没有**。

按 Actions 计费倍率折算（Linux 1× / Windows 2× / macOS 10×），这一次运行吃掉约 **153 分钟额度**，其中约 **133 分钟就是那个挂死的 Windows job**。当天 7 次运行合计约 319 分钟。

## 根因

1. **`X509Store("Root","CurrentUser").Add(...)` 需要交互确认**。dry-run 原本是"生成自签代码签名证书 → 装进当前用户受信任根 → 让 `signtool verify /pa /all` 走真实链路"。把一张证书塞进受信任根存储会触发 Windows 安全确认对话框；GitHub runner 是无人值守会话，**没人点确定**，进程就永久等待，且它不打印任何东西，所以看起来像"构建很慢"。
2. **所有 job 都没设 `timeout-minutes`**。默认上限是 360 分钟，于是一次静默挂死可以合法地烧掉一整小时额度才被上层机制掐掉。CI 的成本防线当时完全依赖"应该不会卡吧"。
3. **`ci.yml` 在 `main` 和 `feat/**` 的每次 push 都跑三平台矩阵**。Windows 2× + macOS 10× 的倍率下，一次普通提交约 15–45 分钟额度；一天 6 次提交就够买一次 dry-run 的量。
4. 顺带一条：`ubuntu-latest` 自 2026-10-19 起是 Ubuntu 26.04，`dpkg-sig` 在该镜像仓库里已不可安装——**runner 镜像滚动会悄悄改变依赖可用性**，这是 Linux job 20 秒失败的原因（不是凭据问题，也不是签名逻辑问题）。

## 修复（机制层面，见 ADR-0018）

- **Windows dry-run 不再触碰任何证书存储**：自签证书在内存里用 `CertificateRequest` 生成后直接导出 PFX；验签改为 `Get-AuthenticodeSignature`，断言"签名存在（看 `SignerCertificate` 是否非空，状态词不作判据：自签证书是 `UnknownError`，未签名文件是 `NotSigned` 且无签名者）+ 签名者就是这张临时证书 + 具备代码签名 EKU"。篡改负向对照见 P-017。`signtool verify /pa /all` 只在真实证书（正式标签）路径执行。EKU 判断按 OID `1.3.6.1.5.5.7.3.3`，**不用 `FriendlyName`**——后者是本地化字符串，中文系统上会变。
- **每个 job 都有 `timeout-minutes`**（plan 5 / check 20 / build 20 / prepare 20 / windows 25 / macos 30 / linux 25 / release 15），两个 workflow 都加 `concurrency` + `cancel-in-progress`。
- **`ci.yml` 取消 push 触发**，改 `workflow_dispatch`（平台可选，默认只 `windows`）+ 指向 main 的 PR；日常门禁搬到本地同源脚本 `bash scripts/gate.sh --race`。
- **`.gitattributes` 补齐 `eol=lf`**（go/ts/tsx/js/css/json/yml/yaml/html/sql/nsh/plist/md/mod/sum）：Windows 上 `core.autocrlf=true` 会把 checkout 出的源码写成 CRLF，导致 `prettier --check` 与 `gofmt -l` 在本机全线误报，并留下一堆"只改换行"的幻影 diff。

## 预防

- 在 CI 里做"只有交互桌面才能完成"的事（证书存储导入、GUI 授权框、需要人按的回车）之前，先假定它会永久阻塞：**任何 job 都要有 `timeout-minutes`**，这是额度防线，不是可选项。
- 自签/临时凭据的验证目标要写清"证明的是什么"：dry-run 证明签名机制可用（结构、签名者、哈希一致），不证明公共信任链可用（那是 A7）。为了后者去装受信任根，等于把验收变成挂死。
- 依赖 runner 镜像里的系统包时，把"镜像滚动可能移除该包"当作已知风险：要么钉镜像版本，要么在 job 里先探测可用性并给出明确失败信息。
- 平台优先级要落到流水线默认值上：首发 Windows，就应该默认只跑 Windows，其余平台显式勾选，而不是每次都三平台齐跑。
- 每次远端运行前算一下倍率成本（Linux 1× / Windows 2× / macOS 10×），能用本地脚本得到的证据不要在远端重复。
- 用 Windows PowerShell 5.1 按行解析 UTF-8 文件（YAML、workflow、脚本）时必须显式 `-Encoding UTF8`：默认按 ANSI（936）解码时多字节序列会吞掉后面的换行，实测把 644 行的 `release.yml` 读成 594 行，按行切分步骤会整段错位，进而误判"语法错误"。含中文的 `.ps1` 输出也要小心：5.1 读 BOM-less 文件按 ANSI 解码，所以本机脚本的输出保持 ASCII。

##（如复发）再次发生记录

- （无）

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml`、`.github/workflows/ci.yml`、`scripts/gate.sh`、`.gitattributes`、[ADR-0018](../../docs/adr/ADR-0018-local-first-gate-and-on-demand-ci.md)、`docs/design/phase-4/release-signing.md`。
