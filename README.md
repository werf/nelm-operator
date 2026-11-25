# nelm-operator

**nelm-operator** is a Kubernetes operator that continuously deploys Helm Charts
using [Nelm](https://github.com/werf/nelm), a Helm 4 alternative and the
deployment engine of [werf](https://github.com/werf/werf). You declare a
`Release` custom resource, and the operator installs the chart, keeps it in the
declared state, and uninstalls it when the `Release` is deleted.

Charts are fetched by the [Flux source-controller](https://fluxcd.io/flux/components/source/),
so a chart can come from a Helm repository, an OCI registry, a Git repository or
an S3-compatible bucket. If you already run Flux, the operator reuses its
source-controller.

## Install

Get the [Nelm CLI](https://github.com/werf/nelm#install), then check whether the
cluster already runs Flux:

```sh
kubectl get crd gitrepositories.source.toolkit.fluxcd.io
```

### Cluster without Flux

Install the operator together with its own source-controller:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --set installFluxSourceController=true
```

Run the same command again to upgrade.

### Cluster with Flux

> [!WARNING]
> Do not set `installFluxSourceController=true` next to an existing Flux. A
> second source-controller would reconcile the same objects as yours, and its
> CRDs would overwrite the CRDs of your Flux with a possibly older version.

The operator requires **Flux 2.6 or newer**. Check that this prints `v1` among
the versions:

```sh
kubectl get crd ocirepositories.source.toolkit.fluxcd.io \
  -o jsonpath='{.spec.versions[*].name}'
```

Install the operator alone:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-operator
```

Flux's default NetworkPolicy admits traffic to the source-controller only from
the `flux-system` namespace, so allow the operator to download chart artifacts:

```sh
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-nelm-operator
  namespace: flux-system
spec:
  podSelector:
    matchLabels:
      app: source-controller
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: nelm-operator
      ports:
        - protocol: TCP
          port: http
EOF
```

## Quickstart

1. Create a `Release`:
    ```sh
    kubectl apply -f - <<EOF
    apiVersion: nelm.werf.io/v1alpha1
    kind: Release
    metadata:
      name: podinfo
      namespace: default
    spec:
      targetNamespace: podinfo
      interval: 5m
      chart:
        repo:
          url: https://stefanprodan.github.io/podinfo
          name: podinfo
          version: "6.7.1"
          interval: 10m
      values:
        replicaCount: 2
    EOF
    ```

1. Wait until it is deployed:
    ```console
    $ kubectl get release podinfo
    NAME      READY   STATUS                                 REVISION   AGE
    podinfo   True    Release install complete, revision 1   1          45s
    ```

1. Change `spec.values` to upgrade the release, or delete the `Release` to
   uninstall it.

The Nelm release is named after the `Release` object, so the chart sees
`podinfo` as `.Release.Name`.

All fields are described in `kubectl explain release.spec` and in the commented
example [`config/samples/nelm_v1alpha1_release_full.yaml`](config/samples/nelm_v1alpha1_release_full.yaml).

## Key features

* Charts from Helm repositories, OCI registries, Git repositories and buckets,
  declared inline in `spec.chart`, or your own `HelmChart` or `OCIRepository`
  referenced in `spec.chartRef`;
* values from `spec.values`, ConfigMaps and Secrets, plus Nelm encrypted values
  in `spec.secretValuesFrom`;
* render and diff patches with jq in `spec.renderPatches` and `spec.diffPatches`;
* timeouts, retries and auto-rollback of failed installs;
* impersonation of a ServiceAccount set in `spec.serviceAccountName`;
* everything Nelm does: CRD upgrades, advanced resource ordering and lifecycle,
  readiness tracking, and hundreds of fixed Helm bugs.

## Argo CD

Argo CD can sync `Release` objects from Git like any other manifest.

> [!WARNING]
> With label tracking, Argo CD treats resources labeled
> `app.kubernetes.io/instance: <name>` as part of the Application `<name>`. Most
> charts set this label to the release name, which is the name of the `Release`.
> If a `Release` and an Argo CD Application have the same name, Argo CD may
> delete the chart's resources on prune.

Use one of these settings in the `argocd-cm` ConfigMap, then sync your
Applications:

* annotation tracking, the default since Argo CD 3.0:
  ```yaml
  application.resourceTrackingMethod: annotation
  ```
* label tracking with a label other than `app.kubernetes.io/instance`:
  ```yaml
  application.instanceLabelKey: argocd.argoproj.io/instance
  ```

See [Argo CD resource tracking](https://argo-cd.readthedocs.io/en/stable/user-guide/resource_tracking/)
for details.

## Permissions

The operator has cluster-wide permissions to create any resource, so treat the
ability to create a `Release` as equivalent to cluster-admin. To limit what a
release can deploy, set `spec.serviceAccountName`: Nelm then deploys as that
ServiceAccount. This is not a tenancy boundary: the operator still reads the
referenced ConfigMaps, Secrets and chart sources with its own permissions.

## Configuration

| Value | Default | Description |
| --- | --- | --- |
| `nelm-controller.maxConcurrentReconciles` | `1` | Releases reconciled in parallel. Keep it at `1` if releases use different `spec.secretKeyFrom`. |
| `nelm-controller.defaultServiceAccount` | unset | ServiceAccount to deploy as when a `Release` sets no `spec.serviceAccountName`. |

## Development

```sh
make deploy-source-controller
make dev-deploy IMG=nelm-operator:dev KIND_CLUSTER_NAME=kind
make test       # unit and envtest suites
make test-e2e   # end-to-end suite in a temporary kind cluster
make lint
make manifests generate  # after changing api/v1alpha1
```

## License

Apache 2.0, see [LICENSE](LICENSE).
