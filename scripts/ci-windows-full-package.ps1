#requires -Version 7.0
# Release CI: packs the full package from the signed normal installer of this
# run and the installation data the Linux job collected, and signs it with
# the same Cage lite certificate (cage-release environment). The checksums go
# to SHA256SUMS.full, which the publish job appends to the release's own.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Installer,
    [Parameter(Mandatory)][string]$OfflineData,
    [Parameter(Mandatory)][string]$OutputDirectory
)
. (Join-Path $PSScriptRoot 'windows-common.ps1')
$repository = Split-Path $PSScriptRoot -Parent
if ($env:GITHUB_EVENT_NAME -ne 'workflow_dispatch') { throw 'Release candidates require an explicitly dispatched release workflow.' }
if (-not $env:CAGE_WINDOWS_PFX_BASE64 -or -not $env:CAGE_WINDOWS_PFX_PASSWORD) {
    throw 'Configure CAGE_WINDOWS_PFX_BASE64 and CAGE_WINDOWS_PFX_PASSWORD in the cage-release environment.'
}
# The same import as ci-windows-build.ps1: exactly one code-signing
# certificate with its private key, and it must be the pinned one.
$pfx = Join-Path $env:RUNNER_TEMP ('cage-signing-' + [guid]::NewGuid().ToString('N') + '.pfx')
[IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:CAGE_WINDOWS_PFX_BASE64))
$password = ConvertTo-SecureString $env:CAGE_WINDOWS_PFX_PASSWORD -AsPlainText -Force
$certificates = @(Import-PfxCertificate -FilePath $pfx -CertStoreLocation Cert:\CurrentUser\My -Password $password)
Remove-Item -LiteralPath $pfx -Force
Remove-Item Env:CAGE_WINDOWS_PFX_BASE64, Env:CAGE_WINDOWS_PFX_PASSWORD
$signers = @($certificates | Where-Object {
    $_.HasPrivateKey -and @($_.EnhancedKeyUsageList | Where-Object { $_.ObjectId -eq '1.3.6.1.5.5.7.3.3' }).Count -gt 0
})
if ($signers.Count -ne 1) { throw 'PFX must contain exactly one code-signing certificate with its private key.' }
$pinned = [Security.Cryptography.X509Certificates.X509Certificate2]::new((Join-Path $repository 'packaging/windows/cage-lite-signing.cer'))
if ($signers[0].Thumbprint -ne $pinned.Thumbprint) { throw 'The signing certificate is not the one pinned in packaging/windows/cage-lite-signing.cer.' }
$env:CAGE_WINDOWS_SIGNING_PROVIDER = 'certificate-store'
$env:CAGE_WINDOWS_CERT_SHA1 = $pinned.Thumbprint

& (Join-Path $PSScriptRoot 'install-nsis.ps1') -Destination $env:RUNNER_TEMP
$env:PATH = (Join-Path $env:RUNNER_TEMP 'nsis-3.13') + [IO.Path]::PathSeparator + $env:PATH
# The runner image ships 7-Zip; its folder is not always on PATH.
if (-not (Get-Command 7z -ErrorAction SilentlyContinue)) { $env:PATH += [IO.Path]::PathSeparator + (Join-Path $env:ProgramFiles '7-Zip') }
& (Join-Path $PSScriptRoot 'package-windows-full.ps1') -Installer $Installer -OfflineData $OfflineData -OutputDirectory $OutputDirectory -Sign
Move-Item -LiteralPath (Join-Path $OutputDirectory 'SHA256SUMS') -Destination (Join-Path $OutputDirectory 'SHA256SUMS.full') -Force
Get-ChildItem -LiteralPath $OutputDirectory -File | ForEach-Object { '{0}  {1:N0} bytes' -f $_.Name, $_.Length }
