[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$serviceName = 'YeokchamRelay'
$programFilesDirectory = Join-Path $env:ProgramFiles 'Yeokcham Relay'
$stateDirectory = Join-Path $env:ProgramData 'Yeokcham'
$dataDirectory = Join-Path $stateDirectory 'data'
$configuration = Join-Path $stateDirectory 'relay.conf'
$identity = Join-Path $stateDirectory 'relay.identity'
$relayBinary = Join-Path $programFilesDirectory 'yeokcham-relay.exe'
$serviceBinary = Join-Path $programFilesDirectory 'yeokcham-relay-service.exe'

function Assert-Administrator {
    $principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'administrator privileges are required'
    }
}

function Invoke-Icacls {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    & icacls.exe @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'icacls failed'
    }
}

function Test-ServiceExists {
    & sc.exe query $serviceName *> $null
    return $LASTEXITCODE -eq 0
}

Assert-Administrator
if (Test-ServiceExists) {
    throw "service $serviceName already exists; remove it before installing"
}
if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'yeokcham-relay.exe') -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'yeokcham-relay-service.exe') -PathType Leaf)) {
    throw 'package binaries are missing'
}

New-Item -ItemType Directory -Path $programFilesDirectory -Force | Out-Null
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'yeokcham-relay.exe') -Destination $relayBinary -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'yeokcham-relay-service.exe') -Destination $serviceBinary -Force
New-Item -ItemType Directory -Path $stateDirectory -Force | Out-Null
Invoke-Icacls -Arguments @($stateDirectory, '/inheritance:r', '/grant:r', 'BUILTIN\Administrators:(OI)(CI)F', 'NT AUTHORITY\SYSTEM:(OI)(CI)F', 'NT AUTHORITY\LOCAL SERVICE:(OI)(CI)RX')
New-Item -ItemType Directory -Path $dataDirectory -Force | Out-Null
if (Test-Path -LiteralPath $configuration -and -not (Test-Path -LiteralPath $configuration -PathType Leaf)) {
    throw 'relay configuration path is not a file'
}
if (-not (Test-Path -LiteralPath $configuration -PathType Leaf)) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'relay.conf.example') -Destination $configuration
}
if (Test-Path -LiteralPath $identity -and -not (Test-Path -LiteralPath $identity -PathType Leaf)) {
    throw 'relay identity path is not a file'
}
if (-not (Test-Path -LiteralPath $identity -PathType Leaf)) {
    & $relayBinary generate-identity --output $identity
    if ($LASTEXITCODE -ne 0) {
        throw 'relay identity generation failed'
    }
}
& attrib.exe +R $identity
if ($LASTEXITCODE -ne 0) {
    throw 'relay identity could not be made read-only'
}

Invoke-Icacls -Arguments @($dataDirectory, '/inheritance:r', '/grant:r', 'BUILTIN\Administrators:(OI)(CI)F', 'NT AUTHORITY\SYSTEM:(OI)(CI)F', 'NT AUTHORITY\LOCAL SERVICE:(OI)(CI)M')
Invoke-Icacls -Arguments @($configuration, '/inheritance:r', '/grant:r', 'BUILTIN\Administrators:F', 'NT AUTHORITY\SYSTEM:F', 'NT AUTHORITY\LOCAL SERVICE:R')
Invoke-Icacls -Arguments @($identity, '/inheritance:r', '/grant:r', 'BUILTIN\Administrators:F', 'NT AUTHORITY\SYSTEM:F', 'NT AUTHORITY\LOCAL SERVICE:R')

& sc.exe create $serviceName "binPath= `"$serviceBinary`"" 'type= own' 'start= demand' 'obj= NT AUTHORITY\LocalService' 'displayname= Yeokcham Relay'
if ($LASTEXITCODE -ne 0) {
    throw 'service registration failed'
}
& sc.exe failure $serviceName 'reset= 86400' 'actions= restart/5000/restart/5000/""/0'
if ($LASTEXITCODE -ne 0) {
    & sc.exe delete $serviceName *> $null
    throw 'service recovery configuration failed'
}

Write-Output "installed $serviceName; review $configuration, then run: Start-Service -Name $serviceName"
