# Windows MSIX package

Windows support is MSIX-only, x64, and targets Windows 11 24H2 (`10.0.26100`). The package declares `wiFiControl`, `proximity`, and `runFullTrust` for native Wi-Fi hotspot and Wi-Fi Direct support.

Build and sign an installable package with a PFX whose subject is exactly `CN=Arachne Development`:

```powershell
pwsh -File deploy/client/windows/package.ps1 -Output C:\release\arachne.msix -CertificatePath C:\secrets\arachne.pfx -CertificatePassword $env:WINDOWS_MSIX_CERTIFICATE_PASSWORD
```

The builder rejects non-MSIX outputs, existing outputs, unsigned release requests, invalid certificate subjects, and incomplete manifest payloads. It invokes `MakeAppx`, signs with SHA-256, verifies the signature, and then writes the output.

For a non-installable local structural check only:

```powershell
pwsh -File deploy/client/windows/package.ps1 -Output C:\temp\arachne.msix -SkipSigning
pwsh -File deploy/client/windows/package-test.ps1
```

CI reads `WINDOWS_MSIX_CERTIFICATE_BASE64` and `WINDOWS_MSIX_CERTIFICATE_PASSWORD`. Configure both repository secrets to sign the package; without them, CI produces an explicitly unsigned validation artifact only. Before installing a self-signed development package, install its public certificate into `LocalMachine\TrustedPeople`, then run:

```powershell
Add-AppxPackage -Path C:\release\arachne.msix
```

The certificate subject must match the manifest publisher. See Microsoft’s [MSIX signing guide](https://learn.microsoft.com/en-us/windows/msix/package/sign-msix-package-guide) and [app capability declarations](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/app-capability-declarations).
