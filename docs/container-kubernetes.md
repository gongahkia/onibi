# Container And Kubernetes Posture

Piranesi can normalize local container, image, and Kubernetes evidence into a
separate infrastructure posture report. This complements host posture; it does
not replace a CNAPP, admission controller, runtime eBPF monitor, or cloud
inventory platform.

## Container Images

Assess a local Trivy image JSON fixture:

```bash
piranesi container assess --image tests/fixtures/container/trivy-image.json --output piranesi-container-output
```

Fixture intent and expected finding families are documented in
`tests/fixtures/container/README.md`.

Assess an image reference with local Trivy:

```bash
piranesi container assess --image nginx:latest --output piranesi-container-output
```

The command runs `trivy image --format json --quiet <image>` when `--image` is not
a local file. Findings include vulnerable image packages and image defaults that
run as root when the evidence is visible.

## Running Containers

Assess running local Docker containers:

```bash
piranesi container assess --docker-host local --output piranesi-containers
```

This is read-only: Piranesi runs Docker list/inspect operations and does not
start, stop, mutate, or exec into containers. Findings include privileged mode,
host networking, dangerous mounts, and root execution.

## Kubernetes Manifests

Assess local manifests:

```bash
piranesi k8s assess ./manifests --output piranesi-k8s-output
```

Piranesi parses YAML and JSON manifests locally. Initial deterministic checks
cover privileged security contexts, host networking, public `LoadBalancer` and
`NodePort` services, missing resource limits, root execution, and visible
secret references in environment variables.

Fixture intent and expected finding families are documented in
`tests/fixtures/k8s/README.md`.

## Kubernetes API

When `kubectl` is available, Piranesi can collect a read-only snapshot:

```bash
piranesi k8s assess --kubeconfig ~/.kube/config --namespace default --output piranesi-k8s-output
```

The command uses `kubectl get ... -o json` only. It does not mutate cluster
resources.

## Outputs

Container assessment writes:

```text
container-report.json
container-report.md
```

Kubernetes assessment writes:

```text
k8s-report.json
k8s-report.md
```

These reports reuse host posture concepts such as evidence inventory, posture
score, risk scores, top actions, and evidence-bound findings while keeping
`ContainerImageSnapshot`, `RunningContainerSnapshot`, and `KubernetesSnapshot`
separate from `HostSnapshot`.
