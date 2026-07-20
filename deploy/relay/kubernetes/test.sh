#!/bin/sh

set -eu

die() {
    printf '%s\n' "kubernetes manifest test error: $1" >&2
    exit 1
}

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repository_root=$(CDPATH= cd -- "$script_directory/../../.." && pwd -P)
namespace=yeokcham-relay
client_namespace=yeokcham-relay-client
image=ghcr.io/gongahkia/yeokcham-relay:0.1.0
kind_cluster=yeokcham-relay-manifest-test-$$
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/yeokcham-relay-kubernetes.XXXXXX")

cleanup() {
    kind delete cluster --name "$kind_cluster" >/dev/null 2>&1 || true
    rm -rf -- "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

command -v docker >/dev/null 2>&1 || die 'docker is required'
command -v kind >/dev/null 2>&1 || die 'kind is required'
command -v kubectl >/dev/null 2>&1 || die 'kubectl is required'
[ -f "$repository_root/Cargo.toml" ] || die 'repository root could not be located'
if kind get clusters | grep -Fx "$kind_cluster" >/dev/null 2>&1; then
    die 'test cluster already exists'
fi

kubectl kustomize "$script_directory" >/dev/null
docker build --file "$repository_root/deploy/relay/Dockerfile" --tag "$image" "$repository_root"
kind create cluster --name "$kind_cluster" --wait 120s
kind load docker-image --name "$kind_cluster" "$image"
kubectl config use-context "kind-$kind_cluster" >/dev/null
kubectl apply --filename "$script_directory/namespace.yaml" >/dev/null
kubectl apply --dry-run=server -k "$script_directory" >/dev/null
kubectl apply -k "$script_directory" >/dev/null

pod=yeokcham-relay-0
if kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=20s >/dev/null 2>&1; then
    die 'relay became ready without its required identity secret'
fi

identity_directory="$temporary_directory/identity"
mkdir "$identity_directory"
chmod 777 "$identity_directory"
docker run --rm --volume "$identity_directory:/identity" "$image" generate-identity --output /identity/relay.identity >/dev/null
kubectl -n "$namespace" create secret generic yeokcham-relay-identity --from-file=relay.identity="$identity_directory/relay.identity" >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=180s
kubectl -n "$namespace" exec "$pod" -- /usr/local/bin/yeokcham-relay healthcheck --address 127.0.0.1:8080 >/dev/null

service_ip=$(kubectl -n "$namespace" get service yeokcham-relay --output=jsonpath='{.spec.clusterIP}')
[ -n "$service_ip" ] || die 'relay service has no cluster IP'
if kubectl run denied-client --namespace default --rm --attach --restart=Never --image=busybox:1.37 --command -- sh -c "nc -z -w 3 $service_ip 50051" >/dev/null 2>&1; then
    die 'unlabeled client reached the relay through default-deny ingress'
fi
kubectl create namespace "$client_namespace" >/dev/null
kubectl label namespace "$client_namespace" yeokcham.io/relay-client=true >/dev/null
kubectl run allowed-client --namespace "$client_namespace" --rm --attach --restart=Never --image=busybox:1.37 --command -- sh -c "nc -z -w 5 $service_ip 50051" >/dev/null

kubectl -n "$namespace" delete "pod/$pod" --wait=true >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready "pod/$pod" --timeout=180s
kubectl -n "$namespace" exec "$pod" -- /usr/local/bin/yeokcham-relay healthcheck --address 127.0.0.1:8080 >/dev/null
