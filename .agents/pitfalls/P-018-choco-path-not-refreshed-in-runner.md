# P-018 choco 安装成功，后续步骤仍然找不到命令（runner 进程环境不刷新）

## 现象

run `41156332487` 的 `windows` job 在"签名可执行文件与安装包"步骤失败：

```
"makensis": executable file not found in $PATH
task: Failed to run task "windows:create:nsis:installer": exit status 127
```

但同一次运行的前一步"安装 makensis（NSIS）"日志里，Chocolatey 明确写着 `nsis.install has been installed`、`Deployed to 'C:\Program Files (x86)\NSIS'`，并且该步骤自己的 `Get-Command makensis` 也没有报错路径。

## 根因

- Chocolatey 安装时修改的是**机器级/用户级 PATH 环境变量**（注册表），不会修改已经运行的进程环境。
- GitHub Actions 的 runner agent 是常驻进程，**早于**安装步骤启动；它派生的每个步骤继承的是 agent 自己的环境快照。因此安装步骤之后的步骤里 `PATH` 仍是旧的，`Get-Command makensis` 与 `wails3 task` 拉起子进程时都找不到它。
- 同理，`choco install` 之后**在同一个步骤内**调用新命令也可能失败（同进程环境不刷新）。

## 修复

`release.yml` 的安装步骤不再"装完就算"，而是显式定位可执行文件并写入 `$GITHUB_PATH`：

```powershell
$makensis = (Get-Command makensis -ErrorAction SilentlyContinue).Source
if (-not $makensis) {
  $makensis = @(
    (Join-Path ${env:ProgramFiles(x86)} "NSIS\makensis.exe"),
    (Join-Path $env:ProgramFiles "NSIS\makensis.exe")
  ) | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $makensis) { choco install nsis --no-progress -y; ... }
Split-Path $makensis | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append
```

后续步骤由 runner 把 `GITHUB_PATH` 的内容前置到 `PATH`，`wails3 task` 才能找到 `makensis`。找不到时显式失败，而不是让签名步骤莫名其妙地 127。

## 预防

- 任何"安装工具 → 后续步骤调用"的流程，不能假设安装器会修好当前 job 的 `PATH`：要么用绝对路径调用，要么安装后立即写 `GITHUB_PATH`（并在安装步骤里验证可执行文件真的存在）。
- 同类风险：`npm install -g`、`pip install --user`、`winget install`、手动解压的 CLI。远端 runner 与本地终端不一样，本地"重开一个终端就好了"的直觉在 CI 里不成立。
- 成本上也有收获：这次失败在 5 分钟内显式暴露（有 `timeout-minutes` + 单平台），没有再出现挂一小时。

## 归属

Phase 4 发布流水线（2026-10-31）/ `.github/workflows/release.yml` 的"安装 makensis（NSIS）"与"签名可执行文件与安装包"步骤；与 P-016（runner 环境与成本）、P-015（默认分支限制）同族。
