# TODO - 30 March 2026

## Remaining Implementations, Broken Flows, or Stubs

1. Implement real Ghostty window/session enumeration in `Onibi/Services/GhosttyIPCClient.swift`.
   - Current `getActiveWindows()` path still uses process-level placeholder data instead of Accessibility API window details.

2. Surface diagnostics data in UI layers.
   - `OnibiCore/ViewModels/MobileMonitorViewModel` now fetches `diagnostics`, but there is no explicit diagnostics rendering in `OnibiPhone` views yet.

3. Run full verification in a Swift-capable environment.
   - `swift` toolchain is unavailable in this workspace, so `swift build` and `swift test` have not been executed after the changes.

4. Validate live endpoint integration for `/api/v1/diagnostics`.
   - Confirm end-to-end host/mobile behavior with real pairing token auth and non-empty runtime diagnostics payloads.
