# TODO 18: Add Container, Image, And Kubernetes Evidence Normalization

## Goal

Extend Piranesi from VM/host posture into adjacent local infrastructure surfaces:
container images, running containers, and Kubernetes manifests. This broadens
adoption while staying aligned with evidence normalization and risk explanation.

## Current State

Host mode can ingest Trivy filesystem vulnerability JSON, but it does not model
container images, running containers, Kubernetes resources, or image-to-host risk.

## Desired CLI

Add:

```bash
piranesi container assess --image nginx:latest --output piranesi-container-output
piranesi container assess --docker-host local --output piranesi-containers
piranesi k8s assess ./manifests --output piranesi-k8s-output
piranesi k8s assess --kubeconfig ~/.kube/config --namespace default
```

## Evidence Sources

Support optional ingestion from:

- Trivy image JSON.
- Docker inspect JSON.
- Docker container list.
- Kubernetes manifests.
- Kubernetes API read-only snapshots.
- kube-bench / kube-score output where available.

## Data Model

Add separate but related models:

```python
class ContainerImageSnapshot(BaseModel): ...
class RunningContainerSnapshot(BaseModel): ...
class KubernetesSnapshot(BaseModel): ...
class InfrastructureFinding(BaseModel): ...
```

Do not force everything into `HostSnapshot`. Link infrastructure findings back to
hosts when evidence supports that relationship.

## Findings

Initial deterministic findings:

- Vulnerable image packages.
- Privileged containers.
- Host network mode.
- Dangerous volume mounts.
- Containers running as root.
- Kubernetes workloads with privileged security contexts.
- Public LoadBalancer/NodePort exposure.
- Missing resource limits.
- Secrets mounted as environment variables where visible in manifests.

## Reporting

Reuse report concepts:

- Evidence inventory.
- Collection health.
- Top actions.
- Risk scores if `todo6.md` has landed.
- Control mappings if `todo10.md` has landed.

## Tests

Fixtures:

```text
tests/fixtures/container/
tests/fixtures/k8s/
```

Tests should cover:

- Trivy image JSON ingestion.
- Docker inspect parsing.
- Kubernetes manifest parsing.
- Privileged container finding.
- Public service finding.
- No external cluster required for fixture tests.

## Documentation

Add:

```text
docs/container-kubernetes.md
```

Explain how this complements host posture rather than replacing dedicated CNAPP
platforms.

## Acceptance Criteria

- Piranesi can assess container/Kubernetes fixture evidence locally.
- Reports remain evidence-bound and local-first.
- Host, container, and Kubernetes findings use compatible prioritization concepts.
- No cluster mutation is performed.

## Out Of Scope

- Runtime eBPF monitoring.
- Admission controller.
- Full Kubernetes compliance certification.
- Cloud provider inventory.

## Validation Commands

```bash
uv run pytest tests/test_container_posture.py tests/test_k8s_posture.py
uv run piranesi k8s assess tests/fixtures/k8s --output /tmp/piranesi-k8s
```

