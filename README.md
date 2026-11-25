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
cluster already runs a Flux source-controller, for example as part of Flux:

```sh
kubectl get deployments --all-namespaces | grep source-controller
```

### Cluster without a source-controller

Install the operator together with its own source-controller:

```sh
nelm release install -n nelm-operator -r nelm-operator \
  oci://registry.werf.io/charts/nelm-operator/nelm-operator \
  --set installFluxSourceController=true
```

Run the same command again to upgrade.

### Cluster with a source-controller

> [!WARNING]
> Do not set `installFluxSourceController=true` next to an existing
> source-controller. A second one would reconcile the same objects as yours, and
> its CRDs would overwrite yours with a possibly older version.

The operator requires the source-controller of **Flux 2.6 or newer**. Check
that this prints `v1` among the versions:

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
      chart:
        repo:
          url: https://stefanprodan.github.io/podinfo
          name: podinfo
          version: "6.7.1"
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

All fields are described in the [Reference](#reference).

## Argo CD

To manage `Release` objects with Argo CD, use annotation tracking, the default
since Argo CD 3.0. With label tracking, Argo CD may delete the resources a
`Release` deployed if the `Release` has the same name as an Application.

Set it in the `argocd-cm` ConfigMap, then sync your Applications:

```yaml
data:
  application.resourceTrackingMethod: annotation
```

If you have to keep label tracking, set `application.instanceLabelKey` to a
label of its own, such as `argocd.argoproj.io/instance`, instead.

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

## Reference

All fields of a `Release` are optional unless marked required. Durations are
written like `30s`, `5m` or `1h`. ConfigMaps and Secrets referenced by a
`Release` are read from the namespace of the `Release`.

### `spec`

| Field | Default | Description |
| --- | --- | --- |
| `chart` | | Chart source declared inline, see [Chart sources](#chart-sources). Exactly one of `chart` and `chartRef` is required. |
| `chartRef` | | Existing chart source, see [`spec.chartRef`](#specchartref). |
| `targetNamespace` | namespace of the `Release` | Namespace to deploy the release to. Nelm creates it if needed. The release history is stored there too. |
| `interval` | `1m` | How often the release is checked for drift and redeployed if needed. Also the default `interval` of inline chart sources. |
| `timeout` | `30m` | Timeout of install, rollback and uninstall, unless `install.timeout`, `rollback.timeout` or `uninstall.timeout` is set. |
| `suspend` | `false` | Stop reconciling the release. |
| `values` | | Values as inline YAML. |
| `valuesFrom` | | Values files from ConfigMaps and Secrets, see [Values references](#values-references). Merged after `values`, in order, so later ones win. |
| `noDefaultValues` | `false` | Ignore `values.yaml` of the chart. |
| `setRootContextJSON` | | `key=json` pairs to set in the root template context `$`. Meant for tools that generate `Release` objects; use `values` otherwise. |
| `secretKeyFrom` | | Key to decrypt Nelm encrypted values: `kind: Secret`, `name` and `dataKey` (default `key`). |
| `secretValuesFrom` | | Encrypted values files from ConfigMaps and Secrets, see [Values references](#values-references). |
| `noDefaultSecretValues` | `false` | Ignore `secret-values.yaml` of the chart. |
| `serviceAccountName` | operator's `defaultServiceAccount` | ServiceAccount in the target namespace to deploy as. Without it, the operator deploys with its own permissions. |
| `install`, `rollback`, `uninstall` | | See [Install, rollback and uninstall](#install-rollback-and-uninstall). |
| `extraAnnotations`, `extraLabels` | | Annotations and labels to add to all deployed resources. |
| `runtimeAnnotations`, `runtimeLabels` | | Annotations and labels to add to all deployed resources, which never trigger an update when changed. |
| `releaseInfoAnnotations` | | Annotations to add to the release metadata. |
| `releaseLabels` | | Labels to add to the objects the release history is stored in. |
| `appVersion` | | Override `appVersion` of `Chart.yaml`. |
| `renderPatches` | | jq patches that change rendered resources before they are deployed, see [Patches](#patches). |
| `diffPatches` | | jq patches applied to both the live and the desired resource before they are compared, so the patched fields never trigger an update. They do not change what is deployed. See [Patches](#patches). |
| `tracking` | | See [`spec.tracking`](#spectracking). |
| `validation` | | See [`spec.validation`](#specvalidation). |
| `releaseStorage.historyLimit` | `10` | Number of release revisions to keep. Without `releaseStorage`, all revisions are kept. |
| `typescript.ignoreBundleJS` | `false` | Has no effect: the operator does not render TypeScript charts yet. |
| `provenance` | | Has no effect yet. Use `verify` of the [chart source](#chart-sources) to verify signatures. |

### Chart sources

`spec.chart` takes exactly one of `repo`, `oci`, `git` and `bucket`. The
operator turns it into Flux source objects in the namespace of the `Release`, so
the fields work as described in the [Flux source API](https://fluxcd.io/flux/components/source/),
including the structure of `verify`, `include`, `layerSelector` and `sts`.
Secrets for credentials, certificates and proxy settings are referenced by
`name` and must be in the namespace of the `Release`.

Fields of all four sources:

| Field | Default | Description |
| --- | --- | --- |
| `interval` | `spec.interval` | How often to check the source for a new chart. Required for `bucket`. |
| `timeout` | `60s` | Timeout of fetching from the source. |
| `credentialsFrom.name` | | Secret with credentials. |

`repo`, a Helm repository, or an OCI registry if `url` starts with `oci://`:

| Field | Default | Description |
| --- | --- | --- |
| `url` | | Required. Repository URL: `http://`, `https://` or `oci://`. |
| `name` | | Required. Chart name in the repository. |
| `version` | latest | Chart version or semver range. |
| `valuesFiles`, `ignoreMissingValuesFiles` | | Not supported yet. Use `spec.values` and `spec.valuesFrom` instead. |
| `certificateFrom.name` | | Secret with `ca.crt`, or `tls.crt` and `tls.key`, or all three, for TLS. |
| `passCredentials` | `false` | Send credentials to hosts other than `url` too. |
| `insecure` | `false` | Allow plain HTTP for an OCI registry. |
| `verify` | | Verify the chart signature, for OCI registries only. |

`oci`, a chart stored as an OCI artifact:

| Field | Default | Description |
| --- | --- | --- |
| `url` | | Required. Artifact URL, starting with `oci://`. |
| `tag`, `semver`, `digest` | | Exactly one is required: tag, semver range of tags, or `sha256:` digest. |
| `semverFilter` | | Regex to filter the tags matched by `semver`. |
| `provider` | `generic` | Authentication provider: `generic`, `aws`, `azure` or `gcp`. |
| `serviceAccountName` | | ServiceAccount whose image pull secrets or workload identity to authenticate with. |
| `certificateFrom.name`, `proxySettingsFrom.name` | | Secrets with TLS certificates and proxy settings. |
| `insecure` | `false` | Allow plain HTTP. |
| `ignore` | | Files to exclude, in `.gitignore` format. |
| `layerSelector` | first layer | Layer of the artifact to extract. |
| `verify` | | Verify the artifact signature. |

`git`, a chart in a Git repository:

| Field | Default | Description |
| --- | --- | --- |
| `url` | | Required. Repository URL: `http://`, `https://` or `ssh://`. |
| `path` | | Required. Path to the chart in the repository. |
| `branch`, `tag`, `semver`, `commit`, `ref` | | Exactly one is required: what to check out. |
| `valuesFiles`, `ignoreMissingValuesFiles` | | As for `repo`. |
| `provider` | `generic` | Authentication provider: `generic`, `azure` or `github`. |
| `serviceAccountName` | | ServiceAccount for workload identity, with the `azure` provider. |
| `proxySettingsFrom.name` | | Secret with proxy settings. |
| `submodules` | `false` | Clone submodules too. |
| `sparseCheckout` | | Directories to check out instead of the whole repository. |
| `ignore` | | Files to exclude, in `.gitignore` format. |
| `verify` | | Verify the commit or tag signature. |
| `include` | | Other `GitRepository` objects to include. |
| `reconcileStrategy` | `ChartVersion` | Build a new chart when its `version` changes (`ChartVersion`) or on every new commit (`Revision`). |

`bucket`, a chart in an S3-compatible bucket:

| Field | Default | Description |
| --- | --- | --- |
| `endpoint` | | Required. Object storage address. |
| `bucketName` | | Required. Bucket name. |
| `path` | | Required. Path to the chart in the bucket. |
| `valuesFiles`, `ignoreMissingValuesFiles`, `reconcileStrategy` | | As for `git`. |
| `provider` | `generic` | `generic` for any S3-compatible storage, or `aws`, `gcp`, `azure`. |
| `region`, `prefix` | | Bucket region, and prefix to filter the files by. |
| `serviceAccountName` | | ServiceAccount for workload identity, with a provider other than `generic`. |
| `certificateFrom.name`, `proxySettingsFrom.name` | | Secrets with TLS certificates and proxy settings. |
| `insecure` | `false` | Allow plain HTTP. |
| `ignore` | | Files to exclude, in `.gitignore` format. |
| `sts` | | Security Token Service to get temporary credentials from. |

### `spec.chartRef`

Use a Flux source object that you manage yourself.

| Field | Default | Description |
| --- | --- | --- |
| `kind` | | Required. `HelmChart` or `OCIRepository`. |
| `name` | | Required. Object name. |
| `namespace` | namespace of the `Release` | Object namespace. |
| `apiVersion` | | Ignored. |

### Values references

Items of `valuesFrom` and `secretValuesFrom`:

| Field | Default | Description |
| --- | --- | --- |
| `kind` | | Required. `ConfigMap` or `Secret`. |
| `name` | | Required. Object name. |
| `dataKey` | `values.yaml` | Key holding the values file. |
| `optional` | `false` | Skip the item if the object or key is missing, instead of failing. |

### Install, rollback and uninstall

| Field | Default | Description |
| --- | --- | --- |
| `install.timeout` | `spec.timeout` | Timeout of install and upgrade. |
| `install.retries` | `0` | Retries of a failed reconcile before the `Release` is marked stalled. |
| `install.autoRollback` | `false` | Once the retries of a failed install or upgrade are exhausted, roll back to the last successful revision. |
| `install.noInstallCRDs` | `false` | Skip the CRDs from the `crds/` directory of the chart. |
| `install.templatesAllowDNS` | `false` | Allow DNS lookups in templates. |
| `rollback.timeout` | `spec.timeout` | Timeout of a rollback. |
| `uninstall.timeout` | `spec.timeout` | Timeout of an uninstall. |
| `uninstall.deleteNamespace` | `false` | Delete the target namespace after uninstall, with everything in it. Do not use it if the `Release` itself is in that namespace. |

Shared by `install`, `rollback` and `uninstall`:

| Field | Default | Description |
| --- | --- | --- |
| `deletePropagation` | `Background` | Propagation policy of deletions: `Background`, `Foreground` or `Orphan`. |
| `noRemoveManualChanges` | `false` | Keep fields added with `kubectl edit` that are absent from the chart. |
| `noForceAdoption` | `false` | Fail instead of adopting resources that belong to another release. Not available in `uninstall`. |
| `noDefaultPatches` | `false` | Ignore `patches.yaml` of the chart and its subcharts. `renderPatches` and `diffPatches` still apply. |

### Patches

Items of `renderPatches` and `diffPatches`, applied in order. Rollback and uninstall
apply only `diffPatches`.

| Field | Default | Description |
| --- | --- | --- |
| `patch` | | Required. jq program that takes a resource and returns exactly one object. |
| `type` | `jq` | Patch type. Only `jq` is supported. |
| `match` | all resources | Resources to patch, see below. |

`match` takes `kinds`, `names`, `namespaces`, `groups`, `versions`, `charts`,
`labels` and `annotations`. A resource matches if it matches every field that is
set, and a list field matches if any of its items does. List items are exact
values, case-insensitive for `kinds`, `groups` and `versions`, or anchored
regexes when wrapped in slashes, like `/web-.*/`. `charts`
matches the subchart alias or its path, like `myapp/charts/cache`. `labels` and
`annotations` must all match.

```yaml
renderPatches:
  - match:
      kinds: [Deployment]
      names: ["/web-.*/"]
    patch: .spec.template.spec.priorityClassName = "high"
diffPatches:
  - match:
      kinds: [Deployment]
    patch: del(.spec.replicas)
```

### `spec.tracking`

Nelm tracks deployed resources until they are ready.

| Field | Default | Description |
| --- | --- | --- |
| `readinessTimeout`, `creationTimeout`, `deletionTimeout` | none | How long to wait for a resource to become ready, to be created or to be deleted. |
| `finalTracking` | | Wait for resources that nothing else depends on during rollback and uninstall: on without `tracking`, off with `tracking` unless set to `true`. Install and upgrade always wait for them. |
| `podLogs` | `false` | Has no effect yet. |

### `spec.validation`

Nelm validates rendered resources against Kubernetes JSON schemas before deploying.

| Field | Default | Description |
| --- | --- | --- |
| `noResourceValidation` | `false` | Skip resource validation. |
| `noValuesSchemaValidation` | `false` | Skip validating values against `values.schema.json` of the chart. |
| `skip` | | Resources not to validate, like `kind=Deployment,name=my-app`. Keys: `group`, `version`, `kind`, `name`, `namespace`. |
| `extraSchemas` | | Extra schema sources, as URL templates or absolute paths, preferred over the embedded schemas. |
| `schemaCacheLifetime` | `48h` | How long to cache downloaded schemas. With `validation` set, nothing is cached unless you set it. |

## License

Apache 2.0, see [LICENSE](LICENSE).
