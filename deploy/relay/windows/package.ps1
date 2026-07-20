[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Output,
    [ValidateSet('x86_64-pc-windows-gnu', 'x86_64-pc-windows-msvc')]
    [string]$Target = 'x86_64-pc-windows-msvc',
    [string]$SignToolPath = '',
    [string]$SigningCertificateSubject = '',
    [string]$TimestampUrl = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Read-Exact {
    param([Parameter(Mandatory = $true)]$Stream, [Parameter(Mandatory = $true)][byte[]]$Buffer)

    $offset = 0
    while ($offset -lt $Buffer.Length) {
        $read = $Stream.Read($Buffer, $offset, $Buffer.Length - $offset)
        if ($read -le 0) {
            return $false
        }
        $offset += $read
    }
    return $true
}

function Test-PeFile {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::OpenRead($Path)
    try {
        $dosHeader = New-Object byte[] 64
        if (-not (Read-Exact -Stream $stream -Buffer $dosHeader) -or $dosHeader[0] -ne 0x4d -or $dosHeader[1] -ne 0x5a) {
            return $false
        }
        $peOffset = [BitConverter]::ToInt32($dosHeader, 0x3c)
        if ($peOffset -lt $dosHeader.Length -or $peOffset -gt 4096) {
            return $false
        }
        $gapLength = $peOffset - $dosHeader.Length
        if ($gapLength -gt 0 -and -not (Read-Exact -Stream $stream -Buffer (New-Object byte[] $gapLength))) {
            return $false
        }
        $signature = New-Object byte[] 4
        $machine = New-Object byte[] 2
        return (Read-Exact -Stream $stream -Buffer $signature) -and (Read-Exact -Stream $stream -Buffer $machine) -and $signature[0] -eq 0x50 -and $signature[1] -eq 0x45 -and $signature[2] -eq 0 -and $signature[3] -eq 0 -and $machine[0] -eq 0x64 -and $machine[1] -eq 0x86
    } finally {
        $stream.Dispose()
    }
}

function Get-TargetDirectory {
    param([Parameter(Mandatory = $true)][string]$RepositoryRoot)

    if ([string]::IsNullOrWhiteSpace($env:CARGO_TARGET_DIR)) {
        return (Join-Path $RepositoryRoot 'target')
    }
    if ([IO.Path]::IsPathRooted($env:CARGO_TARGET_DIR)) {
        return $env:CARGO_TARGET_DIR
    }
    return [IO.Path]::GetFullPath((Join-Path $RepositoryRoot $env:CARGO_TARGET_DIR))
}

function Get-YeokchamVersion {
    $metadata = (& cargo metadata --locked --no-deps --format-version 1 | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) {
        throw 'cargo metadata failed'
    }
    $packages = @($metadata.packages | Where-Object { $_.name -eq 'yeokcham-relay' })
    if ($packages.Count -ne 1 -or [string]$packages[0].version -notmatch '^[0-9]+(\.[0-9]+){2}$') {
        throw 'relay package version is invalid'
    }
    return [string]$packages[0].version
}

function Invoke-SignTool {
    param([Parameter(Mandatory = $true)][string]$Binary)

    & $SignToolPath sign /n $SigningCertificateSubject /fd SHA256 /tr $TimestampUrl /td SHA256 $Binary
    if ($LASTEXITCODE -ne 0) {
        throw 'SignTool signing failed'
    }
    & $SignToolPath verify /pa /v $Binary
    if ($LASTEXITCODE -ne 0) {
        throw 'SignTool verification failed'
    }
}

function Test-PackagePayload {
    param([Parameter(Mandatory = $true)][string]$Package, [Parameter(Mandatory = $true)][string]$PackageDirectory, [Parameter(Mandatory = $true)][string]$ExpectedChecksums)

    $archive = [IO.Compression.ZipFile]::OpenRead($Package)
    try {
        $required = @('install.ps1', 'uninstall.ps1', 'relay.conf.example', 'yeokcham-relay-service.exe', 'yeokcham-relay.exe', 'SHA256SUMS.txt')
        foreach ($name in $required) {
            $entry = $archive.GetEntry("$PackageDirectory/$name")
            if ($null -eq $entry -or $entry.Length -le 0) {
                return $false
            }
        }
        foreach ($binaryName in @('yeokcham-relay-service.exe', 'yeokcham-relay.exe')) {
            $entry = $archive.GetEntry("$PackageDirectory/$binaryName")
            $temporaryFile = [IO.Path]::GetTempFileName()
            try {
                $entryStream = $entry.Open()
                try {
                    $fileStream = [IO.File]::Create($temporaryFile)
                    try { $entryStream.CopyTo($fileStream) } finally { $fileStream.Dispose() }
                } finally { $entryStream.Dispose() }
                if (-not (Test-PeFile -Path $temporaryFile)) {
                    return $false
                }
            } finally {
                Remove-Item -LiteralPath $temporaryFile -Force -ErrorAction SilentlyContinue
            }
        }
        $checksumEntry = $archive.GetEntry("$PackageDirectory/SHA256SUMS.txt")
        $reader = [IO.StreamReader]::new($checksumEntry.Open(), [Text.Encoding]::ASCII, $false)
        try {
            return $reader.ReadToEnd() -ceq $ExpectedChecksums
        } finally {
            $reader.Dispose()
        }
    } finally {
        $archive.Dispose()
    }
}

if (-not [IO.Path]::IsPathRooted($Output)) { throw '--output must be absolute' }
$Output = [IO.Path]::GetFullPath($Output)
if ([IO.Path]::GetExtension($Output) -cne '.zip') { throw '--output must end in .zip' }
if (Test-Path -LiteralPath $Output) { throw '--output must not already exist' }
$outputParent = [IO.Path]::GetDirectoryName($Output)
if (-not (Test-Path -LiteralPath $outputParent -PathType Container)) { throw '--output parent directory must exist' }
$signingRequested = -not [string]::IsNullOrWhiteSpace($SignToolPath) -or -not [string]::IsNullOrWhiteSpace($SigningCertificateSubject) -or -not [string]::IsNullOrWhiteSpace($TimestampUrl)
if ($signingRequested -and ([string]::IsNullOrWhiteSpace($SignToolPath) -or [string]::IsNullOrWhiteSpace($SigningCertificateSubject) -or [string]::IsNullOrWhiteSpace($TimestampUrl))) { throw 'SignTool path, certificate subject, and timestamp URL are all required for a signed package' }
if ($signingRequested -and -not (Test-Path -LiteralPath $SignToolPath -PathType Leaf)) { throw 'SignTool path does not exist' }

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot 'Cargo.toml') -PathType Leaf)) { throw 'repository root could not be located' }
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-relay-windows-package-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    Set-Location $repositoryRoot
    & cargo build --release --locked --package yeokcham-relay --bin yeokcham-relay --bin yeokcham-relay-service --target $Target
    if ($LASTEXITCODE -ne 0) { throw 'cargo build failed' }
    $targetDirectory = Get-TargetDirectory -RepositoryRoot $repositoryRoot
    $relayBinary = Join-Path $targetDirectory "$Target/release/yeokcham-relay.exe"
    $serviceBinary = Join-Path $targetDirectory "$Target/release/yeokcham-relay-service.exe"
    foreach ($binary in @($relayBinary, $serviceBinary)) {
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or -not (Test-PeFile -Path $binary)) { throw 'release binary is not an x86-64 PE executable' }
        if ($signingRequested) { Invoke-SignTool -Binary $binary }
    }

    $version = Get-YeokchamVersion
    $packageDirectory = "yeokcham-relay-$version-$Target"
    $payloadRoot = Join-Path $temporaryDirectory $packageDirectory
    New-Item -ItemType Directory -Path $payloadRoot | Out-Null
    Copy-Item -LiteralPath $relayBinary -Destination (Join-Path $payloadRoot 'yeokcham-relay.exe')
    Copy-Item -LiteralPath $serviceBinary -Destination (Join-Path $payloadRoot 'yeokcham-relay-service.exe')
    foreach ($resource in @('install.ps1', 'uninstall.ps1', 'relay.conf.example')) {
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot $resource) -Destination (Join-Path $payloadRoot $resource)
    }
    $checksumNames = @('install.ps1', 'relay.conf.example', 'uninstall.ps1', 'yeokcham-relay-service.exe', 'yeokcham-relay.exe')
    $checksums = foreach ($name in $checksumNames) {
        $hash = (Get-FileHash -LiteralPath (Join-Path $payloadRoot $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $name"
    }
    $checksumText = ($checksums -join "`n") + "`n"
    [IO.File]::WriteAllText((Join-Path $payloadRoot 'SHA256SUMS.txt'), $checksumText, [Text.Encoding]::ASCII)

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $package = Join-Path $temporaryDirectory "$packageDirectory.zip"
    [IO.Compression.ZipFile]::CreateFromDirectory($payloadRoot, $package, [IO.Compression.CompressionLevel]::Optimal, $true)
    if (-not (Test-PackagePayload -Package $package -PackageDirectory $packageDirectory -ExpectedChecksums $checksumText)) { throw 'package payload is invalid' }
    Move-Item -LiteralPath $package -Destination $Output
    Write-Output $Output
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
