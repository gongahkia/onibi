[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Output,
    [ValidateSet('x86_64-pc-windows-msvc')]
    [string]$Target = 'x86_64-pc-windows-msvc',
    [string]$CertificatePath = '',
    [string]$CertificatePassword = '',
    [string]$MakeAppxPath = '',
    [string]$SignToolPath = '',
    [switch]$SkipSigning
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$packagePublisher = 'CN=Yeokcham Development'

function Find-WindowsSdkTool {
    param([Parameter(Mandatory = $true)][string]$Name)

    $roots = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'),
        (Join-Path $env:ProgramFiles 'Windows Kits\10\bin')
    ) | Where-Object { Test-Path -LiteralPath $_ -PathType Container }
    $candidates = @($roots | ForEach-Object { Get-ChildItem -LiteralPath $_ -Filter $Name -File -Recurse -ErrorAction SilentlyContinue })
    if ($candidates.Count -eq 0) {
        throw "$Name from the Windows SDK is required"
    }
    return ($candidates | Sort-Object FullName -Descending | Select-Object -First 1).FullName
}

function Resolve-WindowsSdkTool {
    param([Parameter(Mandatory = $true)][string]$ProvidedPath, [Parameter(Mandatory = $true)][string]$Name)

    if ([string]::IsNullOrWhiteSpace($ProvidedPath)) {
        return Find-WindowsSdkTool -Name $Name
    }
    if (-not [IO.Path]::IsPathRooted($ProvidedPath) -or -not (Test-Path -LiteralPath $ProvidedPath -PathType Leaf)) {
        throw "$Name path is invalid"
    }
    return [IO.Path]::GetFullPath($ProvidedPath)
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

function Get-MsixVersion {
    $metadata = (& cargo metadata --locked --no-deps --format-version 1 | ConvertFrom-Json)
    if ($LASTEXITCODE -ne 0) {
        throw 'cargo metadata failed'
    }
    $packages = @($metadata.packages | Where-Object { $_.name -eq 'yeokcham-cli' })
    if ($packages.Count -ne 1 -or [string]$packages[0].version -notmatch '^([0-9]+)\.([0-9]+)\.([0-9]+)$') {
        throw 'yeokcham-cli version must be numeric semantic versioning'
    }
    return "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"
}

function Assert-PackageCertificate {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$Password)

    $securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
    $pfxData = Get-PfxData -FilePath $Path -Password $securePassword
    $certificates = @($pfxData.EndEntityCertificates | Where-Object { $_.Subject -ceq $packagePublisher })
    if ($certificates.Count -ne 1) {
        throw 'package certificate subject must match the MSIX publisher'
    }
}

function Assert-MsixPayload {
    param(
        [Parameter(Mandatory = $true)][string]$Package,
        [Parameter(Mandatory = $true)][string]$MakeAppx,
        [Parameter(Mandatory = $true)][string]$ExpectedVersion,
        [Parameter(Mandatory = $true)][string]$TemporaryDirectory
    )

    $unpacked = Join-Path $TemporaryDirectory 'unpacked'
    & $MakeAppx unpack /p $Package /d $unpacked /o | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'MakeAppx unpack validation failed'
    }
    [xml]$manifest = Get-Content -LiteralPath (Join-Path $unpacked 'AppxManifest.xml') -Raw
    $namespace = New-Object Xml.XmlNamespaceManager($manifest.NameTable)
    $namespace.AddNamespace('f', 'http://schemas.microsoft.com/appx/manifest/foundation/windows10')
    $namespace.AddNamespace('uap10', 'http://schemas.microsoft.com/appx/manifest/uap/windows10/10')
    $namespace.AddNamespace('rescap', 'http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities')
    $identity = $manifest.SelectSingleNode('/f:Package/f:Identity', $namespace)
    if ($null -eq $identity -or $identity.Publisher -cne $packagePublisher -or $identity.Version -cne $ExpectedVersion -or $identity.ProcessorArchitecture -cne 'x64') {
        throw 'MSIX identity is invalid'
    }
    $targetDeviceFamily = $manifest.SelectSingleNode('/f:Package/f:Dependencies/f:TargetDeviceFamily', $namespace)
    $application = $manifest.SelectSingleNode('/f:Package/f:Applications/f:Application', $namespace)
    if ($null -eq $targetDeviceFamily -or $targetDeviceFamily.Name -cne 'Windows.Desktop' -or $targetDeviceFamily.MinVersion -cne '10.0.26100.0' -or $null -eq $application -or $application.EntryPoint -cne 'Windows.FullTrustApplication' -or $application.GetAttribute('RuntimeBehavior', 'http://schemas.microsoft.com/appx/manifest/uap/windows10/10') -cne 'packagedClassicApp' -or $application.GetAttribute('TrustLevel', 'http://schemas.microsoft.com/appx/manifest/uap/windows10/10') -cne 'mediumIL') {
        throw 'MSIX application declaration is invalid'
    }
    if ($null -eq $manifest.SelectSingleNode('/f:Package/f:Capabilities/f:DeviceCapability[@Name="wiFiControl"]', $namespace) -or $null -eq $manifest.SelectSingleNode('/f:Package/f:Capabilities/f:Capability[@Name="proximity"]', $namespace) -or $null -eq $manifest.SelectSingleNode('/f:Package/f:Capabilities/rescap:Capability[@Name="runFullTrust"]', $namespace)) {
        throw 'MSIX capabilities are invalid'
    }
    foreach ($asset in @('yeokcham.exe', 'Assets\StoreLogo.png', 'Assets\Square150x150Logo.png', 'Assets\Square44x44Logo.png')) {
        if (-not (Test-Path -LiteralPath (Join-Path $unpacked $asset) -PathType Leaf)) {
            throw 'MSIX payload is incomplete'
        }
    }
}

if ($env:OS -ne 'Windows_NT') {
    throw 'Windows is required'
}
if (-not [IO.Path]::IsPathRooted($Output)) {
    throw '--output must be absolute'
}
$Output = [IO.Path]::GetFullPath($Output)
if ([IO.Path]::GetExtension($Output) -cne '.msix') {
    throw '--output must end in .msix'
}
if (Test-Path -LiteralPath $Output) {
    throw '--output must not already exist'
}
$outputParent = [IO.Path]::GetDirectoryName($Output)
if (-not (Test-Path -LiteralPath $outputParent -PathType Container)) {
    throw '--output parent directory must exist'
}
if ($SkipSigning -and (-not [string]::IsNullOrWhiteSpace($CertificatePath) -or -not [string]::IsNullOrWhiteSpace($CertificatePassword))) {
    throw '-SkipSigning cannot be combined with certificate parameters'
}
$signingRequested = -not $SkipSigning
if ($signingRequested -and ([string]::IsNullOrWhiteSpace($CertificatePath) -or [string]::IsNullOrWhiteSpace($CertificatePassword))) {
    throw 'certificate path and password are required unless -SkipSigning is set'
}
if ($signingRequested -and (-not [IO.Path]::IsPathRooted($CertificatePath) -or -not (Test-Path -LiteralPath $CertificatePath -PathType Leaf))) {
    throw 'certificate path is invalid'
}

$repositoryRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$manifestTemplate = Join-Path $PSScriptRoot 'msix\AppxManifest.xml.in'
$assets = Join-Path $PSScriptRoot 'msix\assets'
if (-not (Test-Path -LiteralPath (Join-Path $repositoryRoot 'Cargo.toml') -PathType Leaf) -or -not (Test-Path -LiteralPath $manifestTemplate -PathType Leaf) -or -not (Test-Path -LiteralPath $assets -PathType Container)) {
    throw 'Windows MSIX packaging assets are missing'
}

$makeAppx = Resolve-WindowsSdkTool -ProvidedPath $MakeAppxPath -Name 'makeappx.exe'
$signTool = ''
if ($signingRequested) {
    $CertificatePath = [IO.Path]::GetFullPath($CertificatePath)
    Assert-PackageCertificate -Path $CertificatePath -Password $CertificatePassword
    $signTool = Resolve-WindowsSdkTool -ProvidedPath $SignToolPath -Name 'signtool.exe'
}

$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("yeokcham-msix-" + [IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    Set-Location $repositoryRoot
    & cargo build --release --locked --package yeokcham-cli --bin yeokcham --target $Target
    if ($LASTEXITCODE -ne 0) {
        throw 'cargo build failed'
    }
    $targetDirectory = Get-TargetDirectory -RepositoryRoot $repositoryRoot
    $binary = Join-Path $targetDirectory "$Target\release\yeokcham.exe"
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or (Get-Item -LiteralPath $binary).Length -le 0) {
        throw 'release binary was not produced'
    }
    $staging = Join-Path $temporaryDirectory 'staging'
    New-Item -ItemType Directory -Path $staging | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $staging 'yeokcham.exe')
    Copy-Item -LiteralPath $assets -Destination (Join-Path $staging 'Assets') -Recurse
    $version = Get-MsixVersion
    $manifest = (Get-Content -LiteralPath $manifestTemplate -Raw).Replace('__VERSION__', $version)
    [IO.File]::WriteAllText((Join-Path $staging 'AppxManifest.xml'), $manifest, [Text.UTF8Encoding]::new($false))
    $package = Join-Path $temporaryDirectory 'yeokcham.msix'
    & $makeAppx pack /d $staging /p $package /o | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'MakeAppx pack failed'
    }
    Assert-MsixPayload -Package $package -MakeAppx $makeAppx -ExpectedVersion $version -TemporaryDirectory $temporaryDirectory
    if ($signingRequested) {
        & $signTool sign /fd SHA256 /f $CertificatePath /p $CertificatePassword /v $package | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw 'SignTool signing failed'
        }
        & $signTool verify /pa /v $package | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw 'SignTool verification failed'
        }
    }
    Move-Item -LiteralPath $package -Destination $Output
    Write-Output $Output
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
