Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-Fails {
    param([Parameter(Mandatory = $true)][scriptblock]$Action, [Parameter(Mandatory = $true)][string]$Description)

    try { & $Action | Out-Null } catch { return }
    throw "$Description was accepted"
}

function Test-PeStream {
    param([Parameter(Mandatory = $true)]$Stream)

    $dosHeader = New-Object byte[] 64
    if ($Stream.Read($dosHeader, 0, $dosHeader.Length) -ne $dosHeader.Length -or $dosHeader[0] -ne 0x4d -or $dosHeader[1] -ne 0x5a) { return $false }
    $peOffset = [BitConverter]::ToInt32($dosHeader, 0x3c)
    if ($peOffset -lt $dosHeader.Length -or $peOffset -gt 4096) { return $false }
    $remaining = $peOffset - $dosHeader.Length
    while ($remaining -gt 0) {
        $buffer = New-Object byte[] ([Math]::Min($remaining, 4096))
        $read = $Stream.Read($buffer, 0, $buffer.Length)
        if ($read -le 0) { return $false }
        $remaining -= $read
    }
    $signature = New-Object byte[] 4
    $machine = New-Object byte[] 2
    return $Stream.Read($signature, 0, $signature.Length) -eq $signature.Length -and $Stream.Read($machine, 0, $machine.Length) -eq $machine.Length -and $signature[0] -eq 0x50 -and $signature[1] -eq 0x45 -and $signature[2] -eq 0 -and $signature[3] -eq 0 -and $machine[0] -eq 0x64 -and $machine[1] -eq 0x86
}

$scriptDirectory = Split-Path -Parent $MyInvocation.MyCommand.Path
$packageScript = Join-Path $scriptDirectory 'package.ps1'
$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-relay-windows-package-test-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
$previousLinker = $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER
try {
    Assert-Fails -Action { & $packageScript -Output relative.zip -Target x86_64-pc-windows-gnu } -Description 'relative output'
    Assert-Fails -Action { & $packageScript -Output (Join-Path $temporaryDirectory 'invalid.zip') -Target invalid-target } -Description 'invalid target'
    Assert-Fails -Action { & $packageScript -Output (Join-Path $temporaryDirectory 'partial.zip') -Target x86_64-pc-windows-gnu -SignToolPath missing } -Description 'partial signing configuration'

    $linker = Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue
    if ($null -eq $linker) { throw 'x86_64-w64-mingw32-gcc is required to test the GNU Windows relay package' }
    $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $linker.Name
    $package = Join-Path $temporaryDirectory 'yeokcham-relay.zip'
    & $packageScript -Output $package -Target x86_64-pc-windows-gnu
    if (-not (Test-Path -LiteralPath $package -PathType Leaf) -or (Get-Item -LiteralPath $package).Length -le 0) { throw 'package was not created' }

    $archive = [IO.Compression.ZipFile]::OpenRead($package)
    try {
        $metadata = (& cargo metadata --locked --no-deps --format-version 1 | ConvertFrom-Json)
        if ($LASTEXITCODE -ne 0) { throw 'cargo metadata failed' }
        $relayPackages = @($metadata.packages | Where-Object { $_.name -eq 'yeokcham-relay' })
        if ($relayPackages.Count -ne 1) { throw 'relay package metadata is ambiguous' }
        $prefix = "yeokcham-relay-$($relayPackages[0].version)-x86_64-pc-windows-gnu/"
        foreach ($name in @('install.ps1', 'uninstall.ps1', 'relay.conf.example', 'SHA256SUMS.txt', 'yeokcham-relay-service.exe', 'yeokcham-relay.exe')) {
            $entry = $archive.GetEntry("$prefix$name")
            if ($null -eq $entry -or $entry.Length -le 0) { throw "package entry $name is invalid" }
        }
        foreach ($binaryName in @('yeokcham-relay-service.exe', 'yeokcham-relay.exe')) {
            $stream = $archive.GetEntry("$prefix$binaryName").Open()
            try { if (-not (Test-PeStream -Stream $stream)) { throw "$binaryName is not an x86-64 PE executable" } } finally { $stream.Dispose() }
        }
        $installerReader = [IO.StreamReader]::new($archive.GetEntry("${prefix}install.ps1").Open())
        try { $installer = $installerReader.ReadToEnd() } finally { $installerReader.Dispose() }
        if ($installer -notmatch 'sc\.exe create \$serviceName' -or $installer -match '(?m)^\s*&?\s*(?:Start-Service|sc\.exe start)\b') { throw 'installer does not register a demand-start SCM service' }
        if ($installer -notmatch 'NT AUTHORITY\\LocalService' -or $installer -notmatch 'yeokcham-relay-service\.exe') { throw 'installer does not use the restricted service host' }
        if ($installer -notmatch 'LOCAL SERVICE:\(OI\)\(CI\)M' -or $installer -notmatch 'LOCAL SERVICE:R') { throw 'installer does not protect writable and secret relay state separately' }
    } finally {
        $archive.Dispose()
    }
    Assert-Fails -Action { & $packageScript -Output $package -Target x86_64-pc-windows-gnu } -Description 'existing output'
} finally {
    if ($null -eq $previousLinker) { Remove-Item Env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER -ErrorAction SilentlyContinue } else { $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $previousLinker }
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
