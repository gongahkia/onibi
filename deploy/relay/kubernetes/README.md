# Experimental Kubernetes relay

This is a single-replica StatefulSet for the SQLite-backed relay. It uses one `ReadWriteOnce` 10 GiB claim, `OnDelete` updates, a PodDisruptionBudget of one, immutable versioned configuration, and a required externally managed identity Secret. Do not increase `replicas`; this is not a multi-writer relay deployment.

The namespace enforces the Kubernetes `restricted` Pod Security Standard. The relay runs as UID/GID `65532` with no Linux capabilities, `RuntimeDefault` seccomp, no service-account token, a read-only root filesystem, and a 35-second termination grace period for its 30-second drain deadline. The health and metrics listeners remain loopback-only; probes execute the relay healthcheck inside the container rather than exposing either listener.

Before applying, generate the binary identity on a secure admin host and create the Secret. Its key must be named `relay.identity`:

```sh
arachne-relay generate-identity --output /secure/path/relay.identity
kubectl create namespace arachne-relay
kubectl -n arachne-relay create secret generic arachne-relay-identity --from-file=relay.identity=/secure/path/relay.identity
kubectl apply -k deploy/relay/kubernetes
```

The Secret volume is mounted read-only as group-readable by the relay UID; the relay rejects writable, malformed, and wrong-length identity material. Kubernetes Secret encryption at rest and access-control policy are cluster-admin responsibilities.

The only allowed ingress is TCP/50051 from namespaces labeled `arachne.io/relay-client=true`; all other ingress and all relay egress are denied. Apply a separately reviewed Gateway, LoadBalancer, or policy patch for public exposure. The manifests require a default dynamic `ReadWriteOnce` StorageClass; add an environment-specific Kustomize patch if the cluster has none. Replace the default `ghcr.io/gongahkia/arachne-relay:0.1.0` image with a digest-pinned release image before production use.

Run the real Kind integration test with Docker, Kind, and kubectl installed:

```sh
deploy/relay/kubernetes/test.sh
```

It builds the OCI image, creates an ephemeral cluster, proves missing identity material prevents readiness, starts the relay with a real generated Secret, checks the private health endpoint, verifies denied and allowed client network paths, and restarts the Pod against its persistent claim.
