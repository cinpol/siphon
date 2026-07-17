# Running Siphon on Rook-Ceph

On a [Rook](https://rook.io/) cluster the Ceph config and admin keyring already
live in Kubernetes, so there's nothing for you to create. The manifest
[`deploy/kubernetes/siphon-rook.yaml`](../deploy/kubernetes/siphon-rook.yaml)
generates `/etc/ceph` from Rook's own `rook-ceph-mon` secret and
`rook-ceph-mon-endpoints` configmap — the same way the rook-ceph toolbox does —
then keeps the pod idle so you can attach:

```sh
kubectl -n rook-ceph apply -f deploy/kubernetes/siphon-rook.yaml
kubectl -n rook-ceph exec -it deploy/siphon -- siphon
```

It assumes the default `rook-ceph` namespace and resource names; adjust the
manifest if your cluster differs.

## Services on Rook

Rook manages Ceph daemons through Kubernetes, not the cephadm orchestrator, so
Siphon's **Services** view shows a **read-only daemon inventory** (from
`ceph node ls`) instead of start/stop/restart actions. Everything else —
Dashboard, OSDs, Pools, CRUSH, Flags, PGs — works normally.

## Cleanup

```sh
kubectl -n rook-ceph delete -f deploy/kubernetes/siphon-rook.yaml
```

See also [Running Siphon in Kubernetes](./kubernetes.md) (any external cluster)
and [Running Siphon in Docker](./docker.md).
