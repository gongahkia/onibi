# Windows terminal package

Build a Windows package with PowerShell and locked Rust dependencies:

```powershell
pwsh -File deploy/client/windows/package.ps1 -Output C:\release\yeokcham.zip -Target x86_64-pc-windows-msvc
```

The package contains a versioned directory with `yeokcham.exe` and `SHA256SUMS.txt`. Extract it, verify the hash with `Get-FileHash`, then run `yeokcham.exe`. The builder accepts only 64-bit GNU or MSVC Windows targets, rejects relative, non-`.zip`, and existing output paths, validates the x86-64 PE payload before and after archiving, and writes the archive only after validation.

For cross-build validation from macOS or Linux, install the GNU Rust target and an `x86_64-w64-mingw32-gcc` linker, then run:

```powershell
pwsh -File deploy/client/windows/package-test.ps1
```

Release packages must be Authenticode-signed. On Windows with the Windows SDK `SignTool` and a code-signing certificate in the certificate store:

```powershell
pwsh -File deploy/client/windows/package.ps1 -Output C:\release\yeokcham.zip -Target x86_64-pc-windows-msvc -SignToolPath 'C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe' -SigningCertificateSubject 'Yeokcham, Inc.' -TimestampUrl 'https://timestamp.example.com'
```

The builder signs with SHA-256, timestamps, and verifies the executable before hashing and archiving. Unsigned archives are local build artifacts, not release artifacts. See Microsoft’s [SignTool documentation](https://learn.microsoft.com/en-us/windows/win32/seccrypto/signtool) and [PowerShell ZIP documentation](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.archive/compress-archive?view=powershell-7.6).
