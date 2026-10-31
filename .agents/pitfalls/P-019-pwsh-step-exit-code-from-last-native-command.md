# P-019 "断言全部通过"的 pwsh 步骤仍以退出码 1 结束：$LASTEXITCODE 会变成步骤退出码

## 现象

run `41156918894` 的 `windows` job 在"验签（失败即 job 失败）"步骤红掉，但日志里每一步都通过了：

```
bin\happywork.exe -> status=UnknownError signer=A390...
  tampered -> verify exit=1
D:\a\...\happywork-amd64-installer.exe -> status=UnknownError signer=A390...
  tampered -> verify exit=1
dry-run 验签通过：签名存在、签名者匹配、具备代码签名 EKU、主体篡改报 0x80096010（...）
##[error]Process completed with exit code 1.
```

负向对照故意让 `signtool verify` 失败（这是它的目的），断言也确认了失败原因是 `0x80096010`；脚本没有抛异常、没有 `Write-Error`，却仍然让整个步骤失败。

## 根因

GitHub Actions 的 `shell: pwsh` 实际执行 `pwsh -command ". '{script}'"`。**PowerShell 进程在脚本正常结束时，会把 `$LASTEXITCODE`（最近一次原生命令的退出码）作为进程退出码**——即使脚本最后一条语句是 `Write-Host`。负向对照里最后一次原生命令就是 `signtool verify`，它按设计返回 1，于是"全部通过"的步骤以 1 结束。

注意这与 `$ErrorActionPreference` 无关：没有错误记录，只是退出码沿用了原生命令的结果。本机 `powershell -File script.ps1` 实测退出 0，所以这个问题只在 runner 的 `-command` 形式下暴露（P-016 家族：本地能过 ≠ 远端能过）。

## 修复

- 在"以预期内的失败命令结尾"的脚本末尾显式 `exit 0`（workflow 的验签步骤、`scripts/windows-sign-check.ps1` 都加了，后者注释里说明原因）。
- 正向路径仍靠 `Write-Error` + `exit 1` 失败，不被 `exit 0` 掩盖。

## 预防

- 任何步骤只要**故意执行会失败的命令**（负向测试、`grep` 找不到、`signtool verify` 的篡改对照），结束时都要显式写退出码；不要依赖"最后一条 Write-Host 会把它变成 0"。
- 用 `$LASTEXITCODE` 做断言时，断言完立刻把它保存到变量并在末尾 `exit 0`；或把整段包进 `try/finally`，在 `finally` 之外给出明确结论。
- 排查"日志全绿但步骤红"时，先看最后一条原生命令的退出码，而不是继续找 `Write-Error`。

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml` 的"验签（失败即 job 失败）"步骤、`scripts/windows-sign-check.ps1`；与 P-016、P-018 同族（runner 环境差异）。
