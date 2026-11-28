#requires -Version 7.0
# Builds the full package: the released Windows installer's program files,
# unchanged, plus the installation data a first setup downloads (offline\,
# from cmd/cage-offline-data). The two installers run the same signed
# binaries; only offline\ is new. In-app updates keep using the normal
# installer: they pick it by its exact name (internal/appupdate/catalog).
[CmdletBinding()]
param(
    # The normal installer of this VERSION (Cage-<version>-windows-amd64-setup*.exe).
    [Parameter(Mandatory)][string]$Installer,
    # The data directory written by `cage-offline-data assemble -out`.
    [Parameter(Mandatory)][string]$OfflineData,
    [string]$OutputDirectory,
    # owner/name of the repository the release is published in, for the
    # written offer of the packages' sources. Not $Repository: PowerShell
    # names ignore case, and $repository below is the checkout folder.
    [string]$ReleaseRepository = $env:GITHUB_REPOSITORY,
    [switch]$Sign
)
. (Join-Path $PSScriptRoot 'windows-common.ps1')
$repository = Split-Path $PSScriptRoot -Parent
$version = Get-CageVersion $repository
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repository "_output/releases/$version/windows-full" }
$Installer = [IO.Path]::GetFullPath($Installer)
$OfflineData = [IO.Path]::GetFullPath($OfflineData)
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)

$name = [IO.Path]::GetFileName($Installer)
$pattern = '^Cage-' + [regex]::Escape($version) + '-windows-amd64-setup(?<suffix>(-test-signed|-unsigned)?)\.exe$'
if ($name -notmatch $pattern) { throw "Not the normal installer of $version`: $name" }
$suffix = $Matches.suffix
if ($Sign -and $suffix) { throw 'A signed full package needs the signed release installer.' }

# The data belongs to exactly this version and architecture.
$manifestPath = Join-Path $OfflineData 'manifest.json'
$data = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
if ($data.format -ne 1 -or $data.version -ne $version -or $data.arch -ne 'amd64') {
    throw "Offline data is for $($data.version)/$($data.arch) (format $($data.format)), not $version/amd64."
}

$makeNSIS = Get-BuildCommand @('makensis.exe', 'makensis') -Optional
if (-not $makeNSIS) { throw 'NSIS (makensis) is missing from PATH. See scripts/install-nsis.ps1.' }
$sevenZip = Get-BuildCommand @('7z.exe', '7zz', '7z')
$signTool = $null
if ($Sign) {
    Assert-WindowsSigningConfiguration
    Assert-CageReleaseSignature $Installer
    $signTool = Get-SigningTool
}

$work = Join-Path $repository '_output/windows-amd64/full-package'
if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
New-Item -ItemType Directory -Path $work -Force | Out-Null
try {
    # 7-Zip reads NSIS installers; the program files sit in the folder the
    # script stages them in, named after an NSIS variable ($_55_ and the like).
    $extracted = Join-Path $work 'extracted'
    Invoke-BuildTool $sevenZip @('x', '-tNsis', '-bso0', '-bsp0', "-o$extracted", $Installer)
    $roots = @(Get-ChildItem -LiteralPath $extracted -Directory | Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'Cage.install-id') })
    if ($roots.Count -ne 1) { throw 'The installer does not contain exactly one Cage application folder.' }
    $app = $roots[0].FullName
    if ((Get-Content -LiteralPath (Join-Path $app 'Cage.install-id') -Raw) -ne 'io.github.lee-xeo.claude-cage') { throw 'Invalid Cage app folder in the installer.' }
    if ((Get-Content -LiteralPath (Join-Path $app 'version.txt') -Raw).Trim() -ne $version) { throw 'The installer is not this VERSION.' }
    if (Test-Path -LiteralPath (Join-Path $app 'offline')) { throw 'The installer already carries offline data.' }
    # 7-Zip shows the uninstaller that WriteUninstaller produces as a file of
    # that folder; makensis writes it again.
    $uninstaller = Join-Path $app 'Uninstall.exe'
    if (Test-Path -LiteralPath $uninstaller) { Remove-Item -LiteralPath $uninstaller -Force }

    # Every program file is the one the released installer lists.
    $released = Join-Path $app 'Cage.install-files.json'
    $listed = @((Get-Content -LiteralPath $released -Raw | ConvertFrom-Json).files)
    $present = @(Get-ChildItem -LiteralPath $app -File -Recurse -Force | Where-Object FullName -ne $released)
    if ($present.Count -ne $listed.Count) { throw "The installer holds $($present.Count) files but lists $($listed.Count)." }
    foreach ($entry in $listed) {
        $path = Join-Path $app $entry.path
        if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -ne $entry.size -or
            (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $entry.sha256) {
            throw "Program file differs from the installer's list: $($entry.path)"
        }
    }
    if ($Sign) {
        foreach ($file in @('Cage.exe', 'resources/cage.exe', 'resources/mihomo.exe')) { Assert-CageReleaseSignature (Join-Path $app $file) }
    }

    # The data, checked against its manifest as it is copied in.
    $offline = Join-Path $app 'offline'
    $entries = @(@{ path = 'manifest.json' }) + @($data.files | ForEach-Object { @{ path = $_.path; size = $_.size; sha256 = $_.sha256 } })
    if ($data.image.digest -notmatch '^sha512:(?<hex>[0-9a-f]{128})$') { throw 'The offline image has no SHA512 digest.' }
    $entries += @{ path = $data.image.path; size = $data.image.size; sha512 = $Matches.hex }
    foreach ($entry in $entries) {
        $source = Join-Path $OfflineData $entry.path
        $target = Join-Path $offline $entry.path
        New-Item -ItemType Directory -Path (Split-Path $target -Parent) -Force | Out-Null
        Copy-Item -LiteralPath $source -Destination $target
        if ($entry.ContainsKey('size') -and (Get-Item -LiteralPath $target).Length -ne $entry.size) { throw "Offline file size differs from the manifest: $($entry.path)" }
        foreach ($algorithm in @('sha256', 'sha512')) {
            if ($entry.ContainsKey($algorithm) -and (Get-FileHash -LiteralPath $target -Algorithm $algorithm.ToUpperInvariant()).Hash.ToLowerInvariant() -ne $entry[$algorithm]) {
                throw "Offline file differs from the manifest: $($entry.path)"
            }
        }
    }
    $copied = @(Get-ChildItem -LiteralPath $offline -File -Recurse -Force).Count
    if ($copied -ne $entries.Count) { throw "offline\ holds $copied files, the manifest names $($entries.Count)." }

    # The installer's own list now covers offline\ too, so the installer checks
    # it and uninstalling or updating removes it with the other files.
    $ownership = foreach ($file in (Get-ChildItem -LiteralPath $app -File -Recurse -Force | Where-Object FullName -ne $released)) {
        @{ path = [IO.Path]::GetRelativePath($app, $file.FullName).Replace('\', '/'); sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant(); size = $file.Length }
    }
    @{ version = 1; files = @($ownership | Sort-Object { $_.path }) } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $released -Encoding utf8

    New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
    $artifact = Join-Path $OutputDirectory "Cage-$version-windows-amd64-full-setup$suffix.exe"
    $numericVersion = ($version -split '-')[0] + '.0'
    $arguments = @('/V3', '/INPUTCHARSET', 'UTF8', "/DAPP_DIR=$app", "/DVERSION=$version", "/DVERSION_NUMERIC=$numericVersion", "/DOUTPUT_FILE=$artifact", "/DFILE_MANIFEST=$released")
    if ($Sign) { $arguments += @("/DSIGN_PWSH=$((Get-Process -Id $PID).Path)", "/DSIGN_SCRIPT=$(Join-Path $PSScriptRoot 'sign-windows-file.ps1')") }
    $arguments += (Join-Path $repository 'packaging/windows/Cage.nsi')
    Invoke-BuildTool $makeNSIS $arguments
    Invoke-BuildTool (Get-BuildCommand @('node.exe', 'node')) @(
        (Join-Path $PSScriptRoot 'verify-windows-installer-icon.mjs'),
        $artifact, (Join-Path $repository 'packaging/windows/Cage.ico'), $sevenZip
    )
    if ($Sign) { Sign-CageFile $artifact $signTool }
    # GitHub refuses release files of 2 GiB or more.
    if ((Get-Item -LiteralPath $artifact).Length -ge 2GB) { throw 'The full package reaches the 2 GiB release file limit.' }

    # The Debian and NodeSource packages inside, with their source names and
    # versions, for the corresponding-source notice.
    $packages = $OfflineData.TrimEnd('\', '/') + '.packages.tsv'
    if (-not (Test-Path -LiteralPath $packages -PathType Leaf)) { throw "The package list written by cage-offline-data assemble is missing: $packages" }
    $listing = Join-Path $OutputDirectory "Cage-$version-full-packages.tsv"
    Copy-Item -LiteralPath $packages -Destination $listing -Force
    # Where the sources are, and the written offer for them.
    $issues = if ($ReleaseRepository -match '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { "https://github.com/$ReleaseRepository/issues" } else { "the project's GitHub Issues" }
    $notice = Join-Path $OutputDirectory "Cage-$version-full-sources.txt"
    [IO.File]::WriteAllText($notice, (@(
        "Cage lite $version full package: sources of the bundled Debian and NodeSource packages",
        "Cage lite $version 完整包：所带 Debian 与 NodeSource 软件包的源码",
        '',
        "The full package's installer carries, unmodified, the binary packages built from the source packages listed in",
        "Cage-$version-full-packages.tsv (source package, version, origin). They are installed into the Linux environment as",
        'the normal package would download them.',
        "完整包的安装程序原样带有由 Cage-$version-full-packages.tsv 所列源码包（源码包名、版本、来源）构建的二进制软件包，",
        '在 Linux 环境里像普通包下载它们一样安装。',
        '',
        'Debian: the source of every version stays available at snapshot.debian.org:',
        'Debian：每个版本的源码都保存在 snapshot.debian.org：',
        '  https://snapshot.debian.org/package/<source>/<version>/',
        'NodeSource (nodejs): built from the Node.js release of the same version, https://nodejs.org/dist/',
        'NodeSource（nodejs）：由同版本的 Node.js 发布构建，https://nodejs.org/dist/',
        '',
        'Written offer: for at least three years from the release date, the Cage lite maintainers provide the complete',
        'corresponding source code of any of these packages on request, for no more than the cost of physically performing',
        "the distribution. Ask at $issues.",
        "书面承诺：自发布之日起至少三年内，Cage lite 维护者应要求提供上述任一软件包的完整对应源码，收费不超过实际分发的成本。请在 $issues 提出。"
    ) -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    $sums = foreach ($file in @($artifact, $listing, $notice)) { (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + [IO.Path]::GetFileName($file) }
    [IO.File]::WriteAllLines((Join-Path $OutputDirectory 'SHA256SUMS'), $sums)
    Write-Host "Full package: $artifact ($([math]::Round((Get-Item -LiteralPath $artifact).Length / 1MB)) MiB)"
    if (-not $Sign) { Write-Warning 'Unsigned full package: for tests only.' }
} finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
