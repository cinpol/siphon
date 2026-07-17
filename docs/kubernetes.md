# Running Siphon in Kubernetes

Run Siphon in a pod against **any** Ceph cluster reachable from that pod
(cephadm, manual, or external). For **Rook-Ceph**, see [Running Siphon on
Rook-Ceph](./rook.md) — it auto-wires from Rook's own secrets, so it's simpler.

Siphon is a TUI, so the manifest runs the pod idle and you attach to it:

```sh
kubectl exec -it deploy/siphon -- siphon
```

The pod must have network access to the cluster's MONs.

## Provide the cluster config

Siphon authenticates like the `ceph` CLI: give it the cluster's `ceph.conf` and
admin keyring as a Secret, created from a host where `ceph -s` already works:

```sh
kubectl create secret generic siphon-ceph \
  --from-file=ceph.conf=/etc/ceph/ceph.conf \
  --from-file=ceph.client.admin.keyring=/etc/ceph/ceph.client.admin.keyring
```

## Deploy

The manifest ([`deploy/kubernetes/siphon.yaml`](../deploy/kubernetes/siphon.yaml))
mounts that Secret at `/etc/ceph`, where librados looks by default:

```sh
kubectl apply -f deploy/kubernetes/siphon.yaml
kubectl exec -it deploy/siphon -- siphon
```

## Cleanup

```sh
kubectl delete -f deploy/kubernetes/siphon.yaml
```

See also [Running Siphon in Docker](./docker.md) and [Running Siphon on
Rook-Ceph](./rook.md).
