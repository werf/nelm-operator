# nelm-operator

nelm-operator deploys Helm charts to Kubernetes continuously, from a `Release`
custom resource, using [Nelm](https://github.com/werf/nelm) as the deployment
engine instead of Helm.

Nelm is a Helm 4 alternative and the deployment engine of
[werf](https://github.com/werf/werf): `terraform plan`-like release planning,
proper CRD management, built-in secrets, resource ordering and lifecycle
annotations, live log and event streaming, real readiness tracking, and several
hundred fixed Helm bugs. This operator brings that engine to a GitOps-style
reconciliation loop: you declare a `Release`, and the operator installs it,
keeps it matching the declared state, retries and optionally rolls back a failed
install, and uninstalls it when the resource is deleted.

Charts are fetched by the [Flux source-controller](https://fluxcd.io/flux/components/source/),
so a chart can come from a Helm repository, an OCI registry, a Git repository or
an S3-compatible bucket. If you already run Flux, nelm-operator reuses the
source-controller you have.

## Requirements

- Kubernetes, and the permissions to install CRDs and a cluster-wide role.
- A Flux source-controller. The `nelm-operator` chart can install one for you.
  An existing one must come from **Flux 2.6 or newer** (source-controller
  v1.6 or newer): the operator watches `OCIRepository` objects through the
  `source.toolkit.fluxcd.io/v1` API, whether or not your releases use OCI
  charts, and Flux added that API version in 2.6.
- The [Nelm CLI](https://github.com/werf/nelm#install) or Helm to install the
  charts, or just `kubectl` for the plain manifest bundle.

## Install

Charts are published to `oci://registry.werf.io/charts/nelm-operator`. Install
one of these two:

| Chart | Contents |
| --- | --- |
| `nelm-operator` | `nelm-controller`, plus the Flux source-controller and its CRDs when `installFluxSourceController=true` (off by default) |
| `nelm-controller` | The operator and its `Release` CRD, without a source-controller |

First check whether the cluster already runs a source-controller, for example
as part of Flux:

```sh
kubectl get crd gitrepositories.source.toolkit.fluxcd.io
kubectl get deployments --all-namespaces | grep source-controller
```

### Cluster without Flux

Install the operator together with its own source-controller:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --set installFluxSourceController=true
```

Nelm creates the namespace, installs and upgrades the CRDs, and tracks every
resource to readiness before reporting success. Run the same command again to
upgrade.

### Cluster that already runs Flux

> [!WARNING]
> If a source-controller is already installed, **do not** set
> `installFluxSourceController=true`. Install the `nelm-controller` chart alone.
>
> A second source-controller does not stay out of the first one's way:
>
> - It watches all namespaces, so both controllers reconcile the same
>   `GitRepository`, `HelmRepository`, `HelmChart`, `OCIRepository` and `Bucket`
>   objects and compete over their status, including the artifact URL that
>   consumers download from.
> - The chart ships its own copy of the Flux source CRDs, and Nelm applies them
>   on every install and upgrade. They replace the CRDs of your Flux with the
>   version this chart was built with, which may be older than yours or
>   incompatible with it.

Check that your Flux is 2.6 or newer, so that the list this prints includes
`v1`:

```sh
kubectl get crd ocirepositories.source.toolkit.fluxcd.io \
  -o jsonpath='{.spec.versions[*].name}'
```

Then install the operator alone, and let it reach the source-controller as the
next section describes:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-controller
```

### Reaching Flux's source-controller

This applies to every install method next to an existing Flux. The operator
downloads chart artifacts from the source-controller's Service. By default Flux
installs a NetworkPolicy into `flux-system` that admits traffic to its
controllers only from pods in that namespace, so where the CNI enforces
NetworkPolicies the downloads time out. Let the operator's namespace in,
replacing `nelm-operator` if you installed into another namespace:

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

### With Helm

Helm installs the same charts, if that is what you have. On a cluster without
Flux:

```sh
helm install nelm-operator oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --namespace nelm-operator --create-namespace \
  --set installFluxSourceController=true
```

Next to an existing Flux, install the `nelm-controller` chart instead, and add
the NetworkPolicy above:

```sh
helm install nelm-operator oci://registry.werf.io/charts/nelm-operator/nelm-controller \
  --namespace nelm-operator --create-namespace
```

Helm installs the CRDs but never upgrades them, so before a chart upgrade apply
the `Release` CRD of the new version yourself:

```sh
kubectl apply --server-side --force-conflicts -f \
  https://raw.githubusercontent.com/werf/nelm-operator/<version tag>/config/crd/bases/nelm.werf.io_releases.yaml
```

With `installFluxSourceController=true`, upgrade the Flux source CRDs the same
way, taking them from the source-controller release that matches the
`appVersion` of the new `flux-source-controller` chart:

```sh
kubectl apply --server-side --force-conflicts -f \
  https://github.com/fluxcd/source-controller/releases/download/v<appVersion>/source-controller.crds.yaml
```

### Plain manifests

A manifest bundle is attached to every
[release](https://github.com/werf/nelm-operator/releases) if you would rather not
use a chart at all. It installs the operator alone into the `nelm-operator`
namespace, so the cluster needs a Flux 2.6 or newer source-controller already,
and the NetworkPolicy above:

```sh
kubectl apply -f https://github.com/werf/nelm-operator/releases/latest/download/install.yaml
```

### Uninstall

Delete your `Release` objects first, and wait until they are gone: each one
uninstalls its chart and keeps the `nelm.werf.io/release` finalizer until that
is done. A `Release` still present when the operator is removed stays stuck in
deletion.

```sh
kubectl delete releases.nelm.werf.io --all --all-namespaces
```

Then remove the operator the way you installed it:

```sh
nelm release uninstall -n nelm-operator -r nelm-operator   # Nelm
helm uninstall nelm-operator -n nelm-operator              # Helm
kubectl delete -f https://github.com/werf/nelm-operator/releases/download/<version tag>/install.yaml  # plain manifests
```

Nelm and Helm leave the CRDs in the cluster, while deleting the plain bundle
removes the `Release` CRD and the `nelm-operator` namespace with it. Delete
`releases.nelm.werf.io` yourself if you no longer need it, and the Flux source
CRDs only if no other Flux uses them.

## Deploy something

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

```console
$ kubectl get release podinfo
NAME      READY   STATUS                                 REVISION   AGE
podinfo   True    Release install complete, revision 1   1          45s
```

The operator creates the `HelmRepository` and `HelmChart` objects the
source-controller needs, waits for the artifact, and then runs the Nelm install.
Edit `spec.values` and the release is upgraded; delete the `Release` and the
deployed resources are uninstalled.

The Nelm release is named after the `Release` object, `podinfo` here, so that is
what the chart sees as `.Release.Name`.

`config/samples/nelm_v1alpha1_release_full.yaml` is a commented example of every
supported field, and `kubectl explain release.spec` describes them in the
cluster. The authoritative reference is
[`api/v1alpha1/release_types.go`](api/v1alpha1/release_types.go).

## What a Release can do

- **Chart sources** — `spec.chart` declares a Helm repo, OCI registry, Git repo
  or bucket inline and the operator manages the source objects for you;
  `spec.chartRef` points at a `HelmChart` or `OCIRepository` you manage yourself.
- **Values** — inline `spec.values`, plus `spec.valuesFrom` for ConfigMaps and
  Secrets, plus Nelm's encrypted `spec.secretValuesFrom` with the key in
  `spec.secretKeyFrom`.
- **Patches** — `spec.renderPatches` are jq programs that change what is rendered
  and applied; `spec.diffPatches` are jq programs that normalise live and desired
  objects before drift comparison, so noisy fields never trigger a rollout.
- **Lifecycle** — per-action timeouts, `install.autoRollback`, `install.retries`,
  readiness tracking timeouts, release history limits, provenance verification.
- **Impersonation** — `spec.serviceAccountName` reconciles the release as that
  ServiceAccount instead of the operator's own identity.

## API stability

The operator version follows semantic versioning: within `1.x`, upgrades do not
break your `Release` objects or the chart's values.

The CRD is served as `nelm.werf.io/v1alpha1`, which is a separate promise — the
API is still young, and a future release may add a newer served version with
breaking changes and a conversion path. This split is normal in the ecosystem;
Flux shipped `v2.x` with `v2beta2` CRDs for the same reason.

The deployment engine is Nelm v2, which is itself a pre-release. Where the two
disagree, the operator's API is what this project keeps stable.

## Permissions

The operator holds a cluster-wide `*/*/*` role. A tool that installs arbitrary
charts must be able to create arbitrary resources, so this is the same bargain
Flux's helm-controller makes. Treat the ability to create a `Release` as
equivalent to cluster-admin.

To scope a release down, set `spec.serviceAccountName`: the Nelm deploy then
impersonates that ServiceAccount, so the resources of the release can only be
written where that account may write. `--default-service-account` applies one
cluster-wide by default. This is not a full tenancy boundary — the operator still
reads the referenced ConfigMaps, Secrets and chart sources under its own
identity, so anyone who can create a `Release` can read any Secret in that
Release's own namespace.

## Configuration

The charts expose two of the operator's flags as values, besides the usual
`image`, `replicaCount`, `resources`, `nodeSelector`, `tolerations`, `affinity`,
`serviceAccount` and `prometheus.podMonitor.create`:

| Value | Default | Flag |
| --- | --- | --- |
| `maxConcurrentReconciles` | `1` | `--max-concurrent-reconciles` |
| `defaultServiceAccount` | unset | `--default-service-account` |

In the `nelm-operator` chart they sit under the `nelm-controller:` key, and the
source-controller's values under `flux-source-controller:`. The other flags are
not exposed by the charts; set them in the container args of the kustomize
manifests in `config/manager` or of the plain manifest bundle.

Operator flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--max-concurrent-reconciles` | `1` | Releases reconciled in parallel. Values above 1 are unsafe when releases use different `spec.secretKeyFrom`, because Nelm reads the secret key from a process-global variable. |
| `--watch-all-namespaces` / `--watch-namespace` | all | Restrict the operator to a single namespace. |
| `--default-service-account` | none | ServiceAccount to impersonate when a Release does not name one. |
| `--source-api-group` / `--source-api-version` | `source.toolkit.fluxcd.io` / `v1` | API of the source objects, both the ones read for `spec.chartRef` and the ones created for inline `spec.chart`. Point it at a Flux-compatible fork to use that instead. The operator still watches Flux's own `source.toolkit.fluxcd.io/v1` `HelmChart` and `OCIRepository`, so their CRDs must be installed as well. |
| `--release-storage-driver` | `secret` | Where Helm release metadata lives: `secret`, `configmap` or `sql`. |
| `--graceful-shutdown-timeout` | `600s` | How long in-flight reconciles get on SIGTERM. |
| `--log-level` | `info` | `silent`, `error`, `warning`, `info`, `debug`, `trace`. |

`go run ./cmd/main.go --help` lists the rest.

## Development

```sh
make deploy-source-controller
make dev-deploy IMG=nelm-operator:dev KIND_CLUSTER_NAME=kind
```

`dev-deploy` builds the image, loads it into the kind cluster and applies the
kustomize manifests. Then apply a `Release` as shown above.

```sh
make test       # unit and envtest suites
make test-e2e   # end-to-end suite in its own kind cluster, created and deleted by the target
make lint       # golangci-lint
make manifests generate  # after changing api/v1alpha1
```

`make manifests` regenerates the CRDs and copies them into the chart, so never
edit `config/crd/bases` or `charts/nelm-controller/crds` by hand.

## Releasing

Versions come from [release-please](https://github.com/googleapis/release-please)
reading conventional commits on `main`. Merging its release PR updates
`CHANGELOG.md` and the versions recorded across the charts, the kustomize
manifests and the Makefile, which creates the version tag, which runs the release
workflow that publishes the image, the charts and the installer bundle.

`feat:` bumps the minor version, `fix:` the patch. A breaking change needs a
major bump, so mark it with `!` after the type.

To release a specific version regardless of what the commits imply, add a
trailer:

```sh
git commit --allow-empty -m "chore: release 1.2.0" -m "Release-As: 1.2.0"
```

## License

Apache 2.0, see [LICENSE](LICENSE).
