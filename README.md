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

## Install

The operator installs itself with Nelm. Get the CLI from
[the Nelm installation guide](https://github.com/werf/nelm#installation), then:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --set installFluxSourceController=true
```

Nelm creates the namespace, installs and upgrades the CRDs, and tracks every
resource to readiness before reporting success.

`installFluxSourceController` is off by default, because a cluster that already
runs Flux must not get a second source-controller. Turn it on for a cluster that
has no Flux, as above. If you do run Flux, install the controller alone instead:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-controller
```

Helm works too, if that is what you have. Note that Helm installs the CRDs but
never upgrades them, so apply the new CRD yourself before a chart upgrade:

```sh
helm install nelm-operator oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --namespace nelm-operator --create-namespace \
  --set installFluxSourceController=true
```

A plain manifest bundle is attached to every
[release](https://github.com/werf/nelm-operator/releases) if you would rather not
use a chart at all. It carries the operator alone, so the cluster needs a Flux
source-controller already:

```sh
kubectl apply -f https://github.com/werf/nelm-operator/releases/latest/download/install.yaml
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

`feat:` bumps the minor version, `fix:` the patch. A breaking change needs a
major bump, so mark it with `!` after the type.

To release a specific version regardless of what the commits imply, add a
trailer:

```sh
git commit --allow-empty -m "chore: release 1.2.0" -m "Release-As: 1.2.0"
```

## License

Apache 2.0, see [LICENSE](LICENSE).
