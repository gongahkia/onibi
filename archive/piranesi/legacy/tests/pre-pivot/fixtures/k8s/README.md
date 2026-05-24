# Kubernetes Fixtures

These manifests are deterministic and do not require a live Kubernetes cluster.

- `risky-workload.yaml` contains a host-networked deployment with a privileged
  root container, a visible secret environment reference, missing resource
  limits, and a public `LoadBalancer` service.

Expected finding families:

- `k8s.workload.host_network`
- `k8s.workload.privileged_container`
- `k8s.workload.runs_as_root`
- `k8s.workload.missing_resource_limits`
- `k8s.workload.env_secret_ref`
- `k8s.service.public_exposure`

These fixtures are local posture samples. They complement, but do not replace,
cluster policy engines, CNAPP platforms, admission controllers, or runtime
monitoring.

