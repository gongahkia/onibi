# Zenbu Wasm Component guest SDK 1.0.0

This is the supported source SDK for an Extension API v1 Wasm Component guest.
It packages a pinned WIT snapshot, a small Rust value/registration helper, a
starter package, and the repository's `zenbu-component-package` tool.

The SDK targets exactly `zenbu:plugin@1.0.0`, `wit-bindgen = 0.41.0`, and
`cargo-component = 0.21.1`. `Cargo.lock` is required and builds always use
`--locked`; the tool refuses an unpinned guest, a stale WIT snapshot, an
unexpected Component world, or a different Cargo Component version.

## Start a package

From a Zenbu checkout:

```sh
./scripts/zenbu-component-package.sh new /path/to/my-component
cd /path/to/my-component
/path/to/zenbu/scripts/zenbu-component-package.sh build .
```

`new` refuses to overwrite its destination. It copies the template, pinned
`wit/zenbu-plugin.wit`, `src/zenbu_sdk.rs`, and `Cargo.lock`; change the
package/component IDs before distributing the result. `build` writes
`plugin.wasm` atomically and then runs Zenbu's ordinary `plugin-check` staging
path. No package activates during that check.

`check PACKAGE` validates an existing `plugin.wasm` through the same source
and host validation steps without rebuilding. `verify-sdk` is the inexpensive
repository check for snapshot/template drift.

## Capability boundary

The SDK creates only data passed over the existing Component ABI. It creates no
host import, filesystem, network, process, clock, random, environment, or
terminal access. A manifest capability only lets the guest observe the matching
copied request field or return the matching declarative action. The host still
checks every returned action at invocation time.

The conformance package exercises all eight v1 capabilities. The ordinary M9
suite also stages that exact guest with grants withheld and proves that a
document-edit action is denied at the host boundary. The separate unauthorized
import fixture proves that adding a WIT import does not create authority.

## Versioning and upgrades

The SDK major version equals the supported WIT/API major. A 1.x SDK is pinned
to `zenbu:plugin@1.0.0`; it may receive compatible helper fixes but does not
silently replace the WIT snapshot or loosen tool versions. A breaking WIT or
Extension API change requires a new SDK major and an explicit guest migration.
To upgrade, create or select the new SDK deliberately, review the WIT and
manifest changes, regenerate `Cargo.lock`, rebuild, and run `check`. There is
no dependency resolver, package registry, signing format, marketplace, or
authority expansion in this SDK.

The helper is a copied Rust module rather than a compiled crate because
`wit-bindgen` must generate the final Component export shim in each guest
`cdylib`. The package tool verifies copied helper/WIT source against this SDK
before it builds.
