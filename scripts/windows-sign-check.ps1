# windows-sign-check.ps1 -- check the Windows signing chain locally (ADR-0018: local first).
#
# Why local: this machine is Windows and Windows Actions minutes are billed 2x. Signature
# mechanics should fail here in seconds and for free, not on a remote dry-run. The steps mirror
# release.yml: build a throwaway code-signing cert in memory -> sign a *copy* of the build ->
# verify identity/EKU -> prove a body tamper is rejected as a digest mismatch.
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File scripts\windows-sign-check.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\windows-sign-check.ps1 -TimestampUrl http://timestamp.digicert.com
#
# Never touches a certificate store: importing into Cert:\...\Root raises an interactive
# confirmation dialog, which hangs an unattended runner (P-016).
#
# Console text is intentionally ASCII: Windows PowerShell 5.1 reads BOM-less files as the ANSI
# code page, so non-ASCII output here would render as mojibake.
param(
  [string]$Target = "bin\happywork.exe",
  [string]$TimestampUrl = ""
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

if (-not (Test-Path $Target)) { throw "target not found: $Target -- run 'wails3 build' first" }

# Work on a copy: the owner's A5 run hands out the same bin\happywork.exe, and it must stay unsigned.
$work = Join-Path $env:TEMP ("happywork-sign-check-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $work | Out-Null
try {
  $exe = Join-Path $work "happywork.exe"
  Copy-Item $Target $exe -Force

  # 1) Throwaway code-signing certificate, built in memory, exported straight to PFX.
  $dn = [System.Security.Cryptography.X509Certificates.X500DistinguishedName]::new("CN=HappyWork Local Dry-Run")
  $rsa = [System.Security.Cryptography.RSA]::Create(2048)
  $req = [System.Security.Cryptography.X509Certificates.CertificateRequest]::new(
    $dn, $rsa,
    [System.Security.Cryptography.HashAlgorithmName]::SHA256,
    [System.Security.Cryptography.RSASignaturePadding]::Pkcs1)
  $ekus = [System.Security.Cryptography.OidCollection]::new()
  [void]$ekus.Add([System.Security.Cryptography.Oid]::new("1.3.6.1.5.5.7.3.3"))
  $req.CertificateExtensions.Add([System.Security.Cryptography.X509Certificates.X509EnhancedKeyUsageExtension]::new($ekus, $false))
  $req.CertificateExtensions.Add([System.Security.Cryptography.X509Certificates.X509KeyUsageExtension]::new([System.Security.Cryptography.X509Certificates.X509KeyUsageFlags]::DigitalSignature, $true))
  $cert = $req.CreateSelfSigned([datetime]::Now.AddMinutes(-5), [datetime]::Now.AddHours(4))
  $pfx = Join-Path $work "sign.pfx"
  $pw = "dryrun-only"
  [System.IO.File]::WriteAllBytes($pfx, $cert.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Pfx, $pw))
  Write-Host ("[1/4] temp cert ok, thumbprint={0}" -f $cert.Thumbprint)

  # 2) Sign with the SDK signtool. `wails3 tool sign` is unimplemented on Windows (P-012).
  $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" -ErrorAction SilentlyContinue | Select-Object -Last 1
  if (-not $signtool) {
    $signtool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10" -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
      Where-Object { $_.FullName -match "x64" } | Select-Object -Last 1
  }
  if (-not $signtool) { throw "signtool.exe not found (Windows SDK required)" }
  Write-Host ("[2/4] signer = {0}" -f $signtool.FullName)

  $signArgs = @("sign", "/fd", "SHA256", "/f", $pfx, "/p", $pw)
  if ($TimestampUrl) { $signArgs += @("/tr", $TimestampUrl, "/td", "SHA256") }
  $signArgs += @("/d", "HappyWork local dry-run", $exe)
  & $signtool.FullName @signArgs
  if ($LASTEXITCODE -ne 0) { throw "signing failed (exit $LASTEXITCODE)" }
  Write-Host "[3/4] signed the copy"

  # 3) Verify identity, not trust. Measured: a throwaway self-signed cert reports
  #    Status=UnknownError, an unsigned file reports NotSigned with no SignerCertificate --
  #    so "is it signed" is decided by SignerCertificate + thumbprint, never by the status word.
  #    EKU is checked by OID, not by the localized FriendlyName.
  function Test-SigningEku($c) {
    if (-not $c) { return $false }
    $hasExt = @($c.Extensions | Where-Object { $_.Oid.Value -eq "2.5.29.37" }).Count -gt 0
    $listed = @($c.EnhancedKeyUsages | ForEach-Object { $_.Value }) -contains "1.3.6.1.5.5.7.3.3"
    return ($hasExt -or $listed)
  }

  $sig = Get-AuthenticodeSignature $exe
  Write-Host ("       status={0} signer={1}" -f $sig.Status, $sig.SignerCertificate.Thumbprint)
  if (-not $sig.SignerCertificate) { throw "no parseable signature on the copy (SignerCertificate is empty)" }
  if ($sig.SignerCertificate.Thumbprint -ne $cert.Thumbprint) { throw "signer is not the certificate generated in this run" }
  if (-not (Test-SigningEku $sig.SignerCertificate)) { throw "certificate lacks the code-signing EKU (2.5.29.37 / 1.3.6.1.5.5.7.3.3)" }
  if ($sig.Status -in @("NotSigned", "HashMismatch")) { throw "pristine copy failed its own signature check: $($sig.Status)" }

  # 4) Negative control: a body byte flip must fail as TRUST_E_BAD_DIGEST (0x80096010). Measured:
  #    flipping a byte in the *tail* corrupts the PKCS#7 certificate table itself, so the file
  #    degrades to NotSigned / chain error and the digest is never compared -- that proves
  #    nothing. Tamper the image body at 25% and require the digest error itself.
  $copy = Join-Path $work "tampered.exe"
  Copy-Item $exe $copy -Force
  $bytes = [System.IO.File]::ReadAllBytes($copy)
  $pos = [int]($bytes.Length * 0.25)
  $bytes[$pos] = $bytes[$pos] -bxor 0xFF
  [System.IO.File]::WriteAllBytes($copy, $bytes)
  $prev = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  $verifyOut = (& $signtool.FullName verify /pa /all $copy 2>&1 | ForEach-Object { $_.ToString() }) -join "`n"
  $verifyCode = $LASTEXITCODE
  $ErrorActionPreference = $prev
  if ($verifyCode -eq 0) { throw "tampered copy still verifies: assertions are not real" }
  if ($verifyOut -notmatch "0x80096010") {
    throw ("tampered copy failed for the wrong reason (expected TRUST_E_BAD_DIGEST 0x80096010):`n" + $verifyOut)
  }
  Write-Host "       tamper rejected with 0x80096010 (TRUST_E_BAD_DIGEST)"
} finally {
  Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "PASS: local Windows chain -- temp cert, sign, verify identity/EKU, body tamper -> bad digest."
