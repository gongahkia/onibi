# Container Fixtures

These fixtures are deterministic and do not require a live Docker daemon.

- `trivy-image.json` models a vulnerable image package and image metadata.
- `docker-inspect.json` models a privileged running container with host
  networking, root execution, and a dangerous Docker socket bind mount.
- `docker-list.json` models the list output used to verify public port parsing.

Expected finding families:

- `container.image.vulnerable_package`
- `container.image.runs_as_root`
- `container.runtime.privileged`
- `container.runtime.host_network`
- `container.runtime.dangerous_mount`
- `container.runtime.runs_as_root`

These fixtures complement Trivy/CNAPP/runtime platforms; they do not replace
live registry scanning, admission control, or runtime monitoring.

