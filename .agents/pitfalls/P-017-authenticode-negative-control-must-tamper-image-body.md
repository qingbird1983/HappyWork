# P-017 Authenticode 负向对照要篡改镜像主体，篡改文件尾部证明不了摘要比对

## 现象

`scripts/windows-sign-check.ps1` 第一次跑通后，换一个新构建的 `bin/happywork.exe` 复跑，负向对照突然报：

```
tampered file still verifies as our signature (status UnknownError): assertions are not real
```

篡改方式是把文件**最后 3 个字节**翻转一个。奇怪的是：`Get-AuthenticodeSignature` 在篡改副本上仍返回 `SignerCertificate`（指纹等于临时证书），而 `Status=UnknownError`——与未篡改文件完全相同，所以旧断言判不出区别。

## 根因

两件事叠加，都是 Windows Authenticode 的结构决定的：

1. **文件尾部是 PKCS#7 证书表本身**（`WIN_CERTIFICATE` 结构，签名与公钥都在里面）。改尾部字节是在破坏签名容器，不是在改被签名的内容；解析器可能退回 `NotSigned` 或链错误，`SignerCertificate` 甚至仍能读出。摘要根本没被比对，这种"拒绝"不能证明签名有效。
2. **`UnknownError` 不是"签名无效"**。临时自签证书不在信任链上，`Get-AuthenticodeSignature` 在链检查失败时给出 `UnknownError`，且**不区分**"文件被改过"与"证书没人信"；未签名文件则是 `NotSigned` **且 `SignerCertificate` 为空**。

本机用多位置篡改实测（Windows 11 + PowerShell 5.1 + .NET Framework）：

| 篡改位置 | Get-AuthenticodeSignature | signtool verify /pa /all |
|---|---|---|
| 镜像主体（25% / 60% 处） | `HashMismatch` | `WinVerifyTrust returned error: 0x80096010` |
| 文件尾部（倒数第 3 字节） | `UnknownError`（签名者仍可读） | 链不受信任错误（无摘要错误） |
| 文件尾部（倒数第 300 字节） | `NotSigned`（签名者仍可读） | `No signature found` |

`0x80096010` 就是 `TRUST_E_BAD_DIGEST`，与系统语言、.NET 版本无关。

## 修复

- 负向对照改为**篡改镜像主体**：`$pos = [int]($bytes.Length * 0.25)`，翻转 `$bytes[$pos]`。
- 判据改为 `signtool verify /pa /all` 必须非零退出**且输出包含 `0x80096010`**。只认错误码，不认本地化文案；如果只报链错误，说明摘要没被比对，视为断言失效。
- 正向断言仍用 `Get-AuthenticodeSignature`：`SignerCertificate` 非空 + 指纹匹配 + 代码签名 EKU（OID `1.3.6.1.5.5.7.3.3`），并排除 `NotSigned`/`HashMismatch`。
- 本地脚本只签 `bin\happywork.exe` 的**副本**，保持待验收原件未签名。

## 预防

- 写"篡改必须被拒"的测试时，先问清楚**拒在哪一层**：破坏容器、链检查失败、摘要不匹配是三种不同的结论。能拿到具体错误码（HRESULT）就不要用状态词或本地化消息。
- 负向对照要**先证明它在正向文件上表现为"没被拒绝"**（本机正向文件 `signtool` 报的是链错误，不是摘要错误），否则断言可能只是装饰。
- 这条同样适用于 macOS（`codesign --verify` 改主体必须 `invalid signature`）与 Linux（`gpg --verify` 改内容必须报 BAD signature）；判据都应是"内容/摘要不匹配"，而不是"任何失败"。

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml` 验签步骤、`scripts/windows-sign-check.ps1`、[ADR-0018](../../docs/adr/ADR-0018-local-first-gate-and-on-demand-ci.md)、`docs/design/phase-4/release-signing.md` §4.3。
