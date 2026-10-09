# nelm-operator

> **Alpha.** The `nelm.werf.io/v1alpha1` API will change in incompatible ways, and
> the operator is built on a pre-release of Nelm v2. Do not point it at a cluster
> you care about yet.

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

## Install

```sh
helm install nelm-operator oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --namespace nelm-operator --create-namespace \
  --devel \
  --set installFluxSourceController=true
```

`--devel` is required while the only published versions are prereleases: without
it Helm resolves to the newest stable chart, which is an abandoned `0.0.1`. Pin a
specific one with `--version 1.0.0-alpha.1` instead, once you have picked it.

The CRD ships in the chart's `crds/` directory, which Helm installs but never
upgrades. While the API is in alpha it changes between releases, so apply the new
CRD yourself before upgrading:

```sh
kubectl apply -f https://raw.githubusercontent.com/werf/nelm-operator/main/charts/nelm-controller/crds/nelm.werf.io_releases.yaml
```

`installFluxSourceController` is off by default, because a cluster that already
runs Flux must not get a second source-controller. Turn it on for a cluster that
has no Flux, as above. If you do run Flux, install the controller alone instead:

```sh
helm install nelm-operator oci://registry.werf.io/charts/nelm-operator/nelm-controller \
  --namespace nelm-operator --create-namespace --devel
```

A plain manifest bundle is attached to every
[release](https://github.com/werf/nelm-operator/releases) if you would rather not
use Helm. It carries the operator alone, so the cluster needs a Flux
source-controller already:

```sh
kubectl apply -f https://github.com/werf/nelm-operator/releases/download/v1.0.0-alpha.1/install.yaml
```

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
NAME      READY   STATUS                               REVISION   AGE
podinfo   True    Release install complete, revision 1   1          45s
```

The operator creates the `HelmRepository` and `HelmChart` objects the
source-controller needs, waits for the artifact, and then runs the Nelm install.
Edit `spec.values` and the release is upgraded; delete the `Release` and the
deployed resources are uninstalled.

`config/samples/nelm_v1alpha1_release_full.yaml` is a commented example of every
supported field. The authoritative reference is
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

Operator flags, set as container args:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--max-concurrent-reconciles` | `1` | Releases reconciled in parallel. Values above 1 are unsafe when releases use different `spec.secretKeyFrom`, because Nelm reads the secret key from a process-global variable. |
| `--watch-all-namespaces` / `--watch-namespace` | all | Restrict the operator to a single namespace. |
| `--default-service-account` | none | ServiceAccount to impersonate when a Release does not name one. |
| `--source-api-group` / `--source-api-version` | `source.toolkit.fluxcd.io` / `v1` | API of the source objects, both the ones read for `spec.chartRef` and the ones created for inline `spec.chart`. Point it at a Flux-compatible fork to use that instead. |
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
make test-e2e   # end-to-end suite, needs a dedicated kind cluster
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

The prerelease versioning strategy is configured, so an ordinary `feat:` or
`fix:` bumps `1.0.0-alpha.1` to `1.0.0-alpha.2` rather than proposing a stable
version. Only the first release, and later the move off alpha, need to be asked
for explicitly:

```sh
git commit --allow-empty -m "chore: release 1.0.0-alpha.1" -m "Release-As: 1.0.0-alpha.1"
```

## License

Apache 2.0, see [LICENSE](LICENSE).
