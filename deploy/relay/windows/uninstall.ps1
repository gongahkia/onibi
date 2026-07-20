[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$serviceName = 'YeokchamRelay'
$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'administrator privileges are required'
}

& sc.exe stop $serviceName *> $null
if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 1062) {
    throw 'service stop failed'
}
& sc.exe delete $serviceName
if ($LASTEXITCODE -ne 0) {
    throw 'service deletion failed'
}
Write-Output 'service deleted; relay configuration, identity, and data were retained'
