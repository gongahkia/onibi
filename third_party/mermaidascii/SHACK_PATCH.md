# Local Mermaid renderer patch

Upstream: https://github.com/coreequip/mermaidascii, **v0.2.0**.
Module checksum: `h1:dtawCi2WzinmSeXNSLCctikaSmqM4Y6lk6+ojC3qBgQ=`.

The Go module is replaced with this local copy so release builds reproduce the
patch. Upstream source, tests, fixtures, README, deviations, LICENSE and NOTICE
are retained. No external runtime is required.

`route.go`: bound A* to the occupied-node/endpoint rectangle plus eight grid cells
and at most approximately 8,192 discovered cells. Upstream otherwise searches an
unbounded positive grid when the target is enclosed. Unroutable edges use the
existing upstream fallback. `route_bounds_test.go` covers an enclosed target and
a reachable detour. Keep this patch until an upstream bounded search is adopted.

Run: `go test -race github.com/coreequip/mermaidascii` from the Shack root.
`make check` includes these tests. Package licenses/notices remain in release
license generation through the module replacement.
