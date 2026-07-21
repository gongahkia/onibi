Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-Fails {
    param([Parameter(Mandatory = $true)][scriptblock]$Action, [Parameter(Mandatory = $true)][string]$Description)

    try {
        & $Action | Out-Null
    } catch {
        return
    }
    throw "$Description was accepted"
}

if ($env:OS -ne 'Windows_NT') {
    throw 'Windows is required'
}

$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
$packageScript = Join-Path $scriptDirectory 'package.ps1'
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-msix-test-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    Assert-Fails -Action { & $packageScript -Output relative.msix -SkipSigning } -Description 'relative output'
    Assert-Fails -Action { & $packageScript -Output (Join-Path $temporaryDirectory 'invalid.zip') -SkipSigning } -Description 'non-MSIX output'
    Assert-Fails -Action { & $packageScript -Output (Join-Path $temporaryDirectory 'unsigned.msix') -CertificatePath missing -CertificatePassword password } -Description 'invalid certificate path'

    $package = Join-Path $temporaryDirectory 'yeokcham.msix'
    & $packageScript -Output $package -SkipSigning
    if (-not (Test-Path -LiteralPath $package -PathType Leaf) -or (Get-Item -LiteralPath $package).Length -le 0) {
        throw 'MSIX package was not created'
    }
    Assert-Fails -Action { & $packageScript -Output $package -SkipSigning } -Description 'existing output'
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
