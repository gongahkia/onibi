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
    param(
        [Parameter(Mandatory = $true)]$Stream,
        [Parameter(Mandatory = $true)][byte[]]$Buffer
    )

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

function Test-PeStream {
    param([Parameter(Mandatory = $true)]$Stream)

    $dosHeader = New-Object byte[] 64
    if (-not (Read-Exact -Stream $Stream -Buffer $dosHeader)) {
        return $false
    }
    if ($dosHeader[0] -ne 0x4d -or $dosHeader[1] -ne 0x5a) {
        return $false
    }
    $peOffset = [BitConverter]::ToInt32($dosHeader, 0x3c)
    if ($peOffset -lt $dosHeader.Length -or $peOffset -gt 4096) {
        return $false
    }
    $gapLength = $peOffset - $dosHeader.Length
    if ($gapLength -gt 0) {
        $gap = New-Object byte[] $gapLength
        if (-not (Read-Exact -Stream $Stream -Buffer $gap)) {
            return $false
        }
    }
    $signature = New-Object byte[] 4
    $machine = New-Object byte[] 2
    if (-not (Read-Exact -Stream $Stream -Buffer $signature) -or -not (Read-Exact -Stream $Stream -Buffer $machine)) {
        return $false
    }
    return $signature[0] -eq 0x50 -and $signature[1] -eq 0x45 -and $signature[2] -eq 0 -and $signature[3] -eq 0 -and $machine[0] -eq 0x64 -and $machine[1] -eq 0x86
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
    $packages = @($metadata.packages | Where-Object { $_.name -eq 'yeokcham-cli' })
    if ($packages.Count -ne 1) {
        throw 'yeokcham-cli package metadata is ambiguous'
    }
    $version = [string]$packages[0].version
    if ($version -notmatch '^[0-9]+(\.[0-9]+){2}$') {
        throw 'binary version must be numeric semantic versioning'
    }
    return $version
}

function Test-PackagePayload {
    param(
        [Parameter(Mandatory = $true)][string]$Package,
        [Parameter(Mandatory = $true)][string]$PackageDirectory,
        [Parameter(Mandatory = $true)][string]$ExpectedHash
    )

    $archive = [IO.Compression.ZipFile]::OpenRead($Package)
    try {
        $binaryEntry = $archive.GetEntry("$PackageDirectory/yeokcham.exe")
        $checksumEntry = $archive.GetEntry("$PackageDirectory/SHA256SUMS.txt")
        if ($null -eq $binaryEntry -or $null -eq $checksumEntry -or $binaryEntry.Length -le 0) {
            return $false
        }
        $binaryStream = $binaryEntry.Open()
        try {
            if (-not (Test-PeStream -Stream $binaryStream)) {
                return $false
            }
        } finally {
            $binaryStream.Dispose()
        }
        $reader = New-Object IO.StreamReader($checksumEntry.Open(), [Text.Encoding]::ASCII, $false)
        try {
            return $reader.ReadToEnd().Trim() -ceq "$ExpectedHash  yeokcham.exe"
        } finally {
            $reader.Dispose()
        }
    } finally {
        $archive.Dispose()
    }
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

if (-not [IO.Path]::IsPathRooted($Output)) {
    throw '--output must be absolute'
}
$Output = [IO.Path]::GetFullPath($Output)
if ([IO.Path]::GetExtension($Output) -cne '.zip') {
    throw '--output must end in .zip'
}
if (Test-Path -LiteralPath $Output) {
    throw '--output must not already exist'
}
$outputParent = [IO.Path]::GetDirectoryName($Output)
if (-not (Test-Path -LiteralPath $outputParent -PathType Container)) {
    throw '--output parent directory must exist'
}
$signingRequested = -not [string]::IsNullOrWhiteSpace($SignToolPath) -or -not [string]::IsNullOrWhiteSpace($SigningCertificateSubject) -or -not [string]::IsNullOrWhiteSpace($TimestampUrl)
if ($signingRequested -and ([string]::IsNullOrWhiteSpace($SignToolPath) -or [string]::IsNullOrWhiteSpace($SigningCertificateSubject) -or [string]::IsNullOrWhiteSpace($TimestampUrl))) {
    throw 'SignTool path, certificate subject, and timestamp URL are all required for a signed package'
}
if ($signingRequested -and -not (Test-Path -LiteralPath $SignToolPath -PathType Leaf)) {
    throw 'SignTool path does not exist'
}

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot 'Cargo.toml') -PathType Leaf)) {
    throw 'repository root could not be located'
}

$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-windows-package-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    Set-Location $repositoryRoot
    & cargo build --release --locked --package yeokcham-cli --bin yeokcham --target $Target
    if ($LASTEXITCODE -ne 0) {
        throw 'cargo build failed'
    }

    $targetDirectory = Get-TargetDirectory -RepositoryRoot $repositoryRoot
    $binary = Join-Path $targetDirectory "$Target/release/yeokcham.exe"
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or (Get-Item -LiteralPath $binary).Length -le 0) {
        throw 'release binary was not produced'
    }
    $binaryStream = [IO.File]::OpenRead($binary)
    try {
        if (-not (Test-PeStream -Stream $binaryStream)) {
            throw 'release binary is not an x86-64 PE executable'
        }
    } finally {
        $binaryStream.Dispose()
    }
    if ($signingRequested) {
        Invoke-SignTool -Binary $binary
    }

    $version = Get-YeokchamVersion
    $packageDirectory = "yeokcham-$version-$Target"
    $payloadRoot = Join-Path $temporaryDirectory $packageDirectory
    New-Item -ItemType Directory -Path $payloadRoot | Out-Null
    $packagedBinary = Join-Path $payloadRoot 'yeokcham.exe'
    Copy-Item -LiteralPath $binary -Destination $packagedBinary
    $hash = (Get-FileHash -LiteralPath $packagedBinary -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText((Join-Path $payloadRoot 'SHA256SUMS.txt'), "$hash  yeokcham.exe`n", [Text.Encoding]::ASCII)

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $package = Join-Path $temporaryDirectory ("$packageDirectory.zip")
    [IO.Compression.ZipFile]::CreateFromDirectory($payloadRoot, $package, [IO.Compression.CompressionLevel]::Optimal, $true)
    if (-not (Test-PackagePayload -Package $package -PackageDirectory $packageDirectory -ExpectedHash $hash)) {
        throw 'package payload is invalid'
    }
    Move-Item -LiteralPath $package -Destination $Output
    Write-Output $Output
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
