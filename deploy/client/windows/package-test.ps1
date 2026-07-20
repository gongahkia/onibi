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

function Test-PeStream {
    param([Parameter(Mandatory = $true)]$Stream)

    $dosHeader = New-Object byte[] 64
    if (-not (Read-Exact -Stream $Stream -Buffer $dosHeader)) {
        return $false
    }
    $peOffset = [BitConverter]::ToInt32($dosHeader, 0x3c)
    if ($dosHeader[0] -ne 0x4d -or $dosHeader[1] -ne 0x5a -or $peOffset -lt $dosHeader.Length -or $peOffset -gt 4096) {
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
    return (Read-Exact -Stream $Stream -Buffer $signature) -and (Read-Exact -Stream $Stream -Buffer $machine) -and $signature[0] -eq 0x50 -and $signature[1] -eq 0x45 -and $signature[2] -eq 0 -and $signature[3] -eq 0 -and $machine[0] -eq 0x64 -and $machine[1] -eq 0x86
}

$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
$packageScript = Join-Path $scriptDirectory 'package.ps1'
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-windows-package-test-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
$previousLinker = $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER
try {
    Assert-Fails -Action { & $packageScript -Output relative.zip -Target x86_64-pc-windows-gnu } -Description 'relative output'
    Assert-Fails -Action { & $packageScript -Output (Join-Path $temporaryDirectory 'invalid.zip') -Target x86_64-pc-windows-gnu -SignToolPath missing } -Description 'partial signing configuration'

    $linker = Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue
    if ($null -eq $linker) {
        throw 'x86_64-w64-mingw32-gcc is required to test the GNU Windows package'
    }
    $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $linker.Name
    $package = Join-Path $temporaryDirectory 'yeokcham.zip'
    & $packageScript -Output $package -Target x86_64-pc-windows-gnu
    if (-not (Test-Path -LiteralPath $package -PathType Leaf) -or (Get-Item -LiteralPath $package).Length -le 0) {
        throw 'package was not created'
    }

    $archive = [IO.Compression.ZipFile]::OpenRead($package)
    try {
        $binaryEntry = @($archive.Entries | Where-Object { $_.FullName -match '^yeokcham-[0-9]+\.[0-9]+\.[0-9]+-x86_64-pc-windows-gnu/yeokcham\.exe$' })
        $checksumEntry = @($archive.Entries | Where-Object { $_.FullName -match '^yeokcham-[0-9]+\.[0-9]+\.[0-9]+-x86_64-pc-windows-gnu/SHA256SUMS\.txt$' })
        if ($binaryEntry.Count -ne 1 -or $checksumEntry.Count -ne 1) {
            throw 'package entries are invalid'
        }
        $binaryStream = $binaryEntry[0].Open()
        try {
            if (-not (Test-PeStream -Stream $binaryStream)) {
                throw 'package binary is not an x86-64 PE executable'
            }
        } finally {
            $binaryStream.Dispose()
        }
    } finally {
        $archive.Dispose()
    }

    Assert-Fails -Action { & $packageScript -Output $package -Target x86_64-pc-windows-gnu } -Description 'existing output'
} finally {
    if ($null -eq $previousLinker) {
        Remove-Item Env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER -ErrorAction SilentlyContinue
    } else {
        $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $previousLinker
    }
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
