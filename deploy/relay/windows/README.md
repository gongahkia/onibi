# Windows relay service package

Build a Windows relay service ZIP with locked dependencies:

```powershell
pwsh -File deploy/relay/windows/package.ps1 -Output C:\release\arachne-relay.zip -Target x86_64-pc-windows-msvc
```

Extract the archive, verify every `SHA256SUMS.txt` entry with `Get-FileHash`, then run `install.ps1` from an elevated PowerShell session. The installer creates a demand-start `ArachneRelay` SCM service under `NT AUTHORITY\LocalService`, writes protected state in `C:\ProgramData\Arachne`, preserves an existing configuration and identity, and does not start the service. Review `relay.conf`, configure the host firewall for the public relay listener if required, then start the service with `Start-Service -Name ArachneRelay`.

`uninstall.ps1` deletes the service but retains relay configuration, identity, and SQLite data. It does not delete private key material.

For cross-build package validation from macOS or Linux, install the GNU target and `x86_64-w64-mingw32-gcc`, then run:

```powershell
pwsh -File deploy/relay/windows/package-test.ps1
```

Release artifacts require Authenticode signatures for both executables:

```powershell
pwsh -File deploy/relay/windows/package.ps1 -Output C:\release\arachne-relay.zip -Target x86_64-pc-windows-msvc -SignToolPath 'C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe' -SigningCertificateSubject 'Arachne, Inc.' -TimestampUrl 'https://timestamp.example.com'
```

Unsigned ZIPs are local build artifacts, not release artifacts. The package builder signs, timestamps, and verifies both PE files before hashing and archiving. See Microsoft’s [service registration documentation](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/sc-create) and [SignTool documentation](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool).
