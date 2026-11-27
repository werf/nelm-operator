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

Every field of a `Release`, with its default:

```yaml
# Every field of a Release, with its default. Only spec.chart or spec.chartRef is
# required. Durations are written like 30s, 5m or 1h. ConfigMaps and Secrets
# are read from the namespace of the Release.
apiVersion: nelm.werf.io/v1alpha1
kind: Release
metadata:
  # Also the name of the Nelm release, so the chart sees it as .Release.Name.
  name: myapp
  namespace: default
spec:
  # Namespace to deploy to. Nelm creates it if needed and stores the release
  # history there. Default: the namespace of the Release.
  targetNamespace: myapp
  # How often to check for drift and redeploy. Also the default interval of
  # the chart source.
  interval: 1m
  # Timeout of install, rollback and uninstall, unless set for them below.
  timeout: 30m
  # Stop reconciling the release.
  suspend: false

  # Chart source declared inline: exactly one of repo, oci, git and bucket.
  # The operator creates Flux source objects for it in the namespace of the
  # Release, so the fields work as in the Flux source API:
  # https://fluxcd.io/flux/components/source/
  # Secrets in *From fields must be in the namespace of the Release.
  chart:
    # A Helm repository, or an OCI registry if the url starts with oci://.
    repo:
      url: https://stefanprodan.github.io/podinfo  # required
      name: podinfo                 # required, chart name in the repository
      version: "6.7.1"              # version or semver range, default: latest
      interval: 1m                  # default: spec.interval
      timeout: 60s
      credentialsFrom:              # Secret with username and password
        name: repo-credentials
      certificateFrom:              # Secret with ca.crt and/or tls.crt and tls.key
        name: repo-tls
      passCredentials: false        # send credentials to hosts other than url too
      insecure: false               # allow plain HTTP to an OCI registry
      # verify:                     # verify the chart signature, OCI registries only
      #   provider: cosign          # cosign or notation
      #   secretRef:
      #     name: cosign-public-keys
      # Not supported yet: use values and valuesFrom instead.
      # valuesFiles: [values.yaml, values-prod.yaml]
      # ignoreMissingValuesFiles: false

    # A chart stored as an OCI artifact.
    # oci:
    #   url: oci://ghcr.io/stefanprodan/charts/podinfo  # required
    #   tag: "6.7.1"                # exactly one of tag, semver and digest
    #   # semver: "6.7.x"
    #   # digest: sha256:...
    #   semverFilter: ""            # regex to filter the tags matched by semver
    #   interval: 1m                # default: spec.interval
    #   timeout: 60s
    #   provider: generic           # generic, aws, azure or gcp
    #   serviceAccountName: ""      # ServiceAccount to authenticate with
    #   credentialsFrom:            # Secret of type kubernetes.io/dockerconfigjson
    #     name: registry-credentials
    #   certificateFrom:
    #     name: registry-tls
    #   proxySettingsFrom:          # Secret with address, username and password
    #     name: proxy
    #   insecure: false             # allow plain HTTP
    #   ignore: ""                  # files to exclude, in .gitignore format
    #   layerSelector:              # default: the first layer, extracted
    #     mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip
    #     operation: copy           # extract or copy
    #   verify:                     # verify the artifact signature
    #     provider: cosign          # cosign or notation
    #     secretRef:
    #       name: cosign-public-keys

    # A chart in a Git repository.
    # git:
    #   url: https://github.com/stefanprodan/podinfo  # required, http(s):// or ssh://
    #   path: ./charts/podinfo      # required, path to the chart
    #   branch: master              # exactly one of branch, tag, semver, commit and ref
    #   # tag: 6.7.1
    #   # semver: 6.7.x
    #   # commit: 0123abcd...
    #   # ref: refs/heads/master
    #   interval: 1m                # default: spec.interval
    #   timeout: 60s
    #   provider: generic           # generic, azure or github
    #   serviceAccountName: ""      # for workload identity with the azure provider
    #   credentialsFrom:            # Secret with username and password, bearerToken,
    #     name: git-credentials     # or identity and known_hosts for SSH
    #   proxySettingsFrom:
    #     name: proxy
    #   submodules: false           # clone submodules too
    #   sparseCheckout: []          # directories to check out, default: all
    #   ignore: ""                  # files to exclude, in .gitignore format
    #   verify:                     # verify the commit or tag signature
    #     mode: HEAD                # HEAD, Tag or TagAndHEAD
    #     secretRef:
    #       name: pgp-public-keys
    #   include:                    # other GitRepository objects to include
    #     - repository:
    #         name: shared-charts
    #       fromPath: charts
    #       toPath: shared
    #   # Build a new chart when its version changes (ChartVersion) or on every
    #   # new commit (Revision).
    #   reconcileStrategy: ChartVersion
    #   # Not supported yet: use values and valuesFrom instead.
    #   # valuesFiles: [values.yaml, values-prod.yaml]
    #   # ignoreMissingValuesFiles: false

    # A chart in an S3-compatible bucket.
    # bucket:
    #   endpoint: minio.example.com # required
    #   bucketName: charts          # required
    #   path: ./podinfo             # required, path to the chart
    #   interval: 1m                # required
    #   timeout: 60s
    #   provider: generic           # generic (any S3-compatible), aws, gcp or azure
    #   region: us-east-1
    #   prefix: ""                  # prefix to filter the files by
    #   serviceAccountName: ""      # for workload identity, not with generic
    #   credentialsFrom:            # Secret with accesskey and secretkey for generic
    #     name: bucket-credentials
    #   certificateFrom:
    #     name: bucket-tls
    #   proxySettingsFrom:
    #     name: proxy
    #   insecure: false             # allow plain HTTP
    #   ignore: ""                  # files to exclude, in .gitignore format
    #   sts:                        # Security Token Service for temporary credentials
    #     provider: aws             # aws, or ldap with the generic provider
    #     endpoint: https://sts.amazonaws.com
    #   reconcileStrategy: ChartVersion  # ChartVersion or Revision
    #   # Not supported yet: use values and valuesFrom instead.
    #   # valuesFiles: [values.yaml, values-prod.yaml]
    #   # ignoreMissingValuesFiles: false

  # Instead of chart: a Flux source object that you manage yourself.
  # chartRef:
  #   kind: HelmChart               # required, HelmChart or OCIRepository
  #   name: my-chart                # required
  #   namespace: flux-system        # default: the namespace of the Release

  # Values, merged in this order, later ones win: values.yaml of the chart,
  # values, then valuesFrom.
  values:
    replicaCount: 2
  valuesFrom:
    - kind: ConfigMap               # required, ConfigMap or Secret
      name: myapp-values            # required
      dataKey: values.yaml          # key holding the values file
      optional: false               # skip it instead of failing if missing
  noDefaultValues: false            # ignore values.yaml of the chart
  # key=json pairs to set in the root template context $. Meant for tools that
  # generate Release objects; use values otherwise.
  setRootContextJSON:
    - 'environment="production"'

  # Nelm encrypted values, decrypted with the key from secretKeyFrom.
  secretKeyFrom:
    kind: Secret
    name: myapp-secret-key          # required
    dataKey: key
  secretValuesFrom:                 # same fields as valuesFrom
    - kind: Secret
      name: myapp-secret-values
      dataKey: values.yaml
  noDefaultSecretValues: false      # ignore secret-values.yaml of the chart

  # ServiceAccount in the target namespace to deploy as. Default: the
  # operator's --default-service-account, otherwise the operator's own
  # permissions.
  serviceAccountName: myapp-deployer

  install:
    timeout: 30m                    # default: spec.timeout
    retries: 0                      # retries of a failed reconcile before the Release stalls
    # Once the retries of a failed install or upgrade are exhausted, roll back
    # to the last successful revision.
    autoRollback: false
    noInstallCRDs: false            # skip the CRDs from crds/ of the chart
    templatesAllowDNS: false        # allow DNS lookups in templates
    deletePropagation: Background   # Background, Foreground or Orphan
    noRemoveManualChanges: false    # keep fields added with kubectl edit
    noForceAdoption: false          # fail instead of adopting resources of another release
    noDefaultPatches: false         # ignore patches.yaml of the chart and its subcharts
  rollback:
    timeout: 30m                    # default: spec.timeout
    deletePropagation: Background
    noRemoveManualChanges: false
    noForceAdoption: false
    noDefaultPatches: false
  uninstall:
    timeout: 30m                    # default: spec.timeout
    # Delete the target namespace after uninstall, with everything in it. Do
    # not use it if the Release itself is in that namespace.
    deleteNamespace: false
    deletePropagation: Background
    noRemoveManualChanges: false
    noDefaultPatches: false

  extraAnnotations: {}              # added to all deployed resources
  extraLabels: {}
  runtimeAnnotations: {}            # same, but changing them never triggers an update
  runtimeLabels: {}
  releaseInfoAnnotations: {}        # added to the release metadata
  releaseLabels: {}                 # added to the objects storing the release history
  appVersion: ""                    # override appVersion of Chart.yaml

  # jq patches applied to rendered resources before they are deployed, in
  # order. Rollback and uninstall do not apply them.
  renderPatches:
    - match:                        # default: all resources
        kinds: [Deployment]         # exact values, case-insensitive for kinds,
        names: ["/web-.*/"]         # groups and versions, or /anchored regexes/
        # namespaces, groups, versions: same
        # charts: [cache]           # subchart alias, or path like myapp/charts/cache
        # labels: {tier: backend}   # all must match
        # annotations: {}
      type: jq                      # only jq
      patch: .spec.template.spec.priorityClassName = "high"  # must return one object
  # jq patches applied to both the live and the desired resource before they
  # are compared, so the patched fields never trigger an update. They do not
  # change what is deployed. Same fields as renderPatches.
  diffPatches:
    - match:
        kinds: [Deployment]
      patch: del(.spec.replicas)

  # Waiting for deployed resources.
  tracking:
    readinessTimeout: 5m            # default: none
    creationTimeout: 5m             # default: none
    deletionTimeout: 5m             # default: none
    # Wait for resources nothing else depends on, during rollback and
    # uninstall. Default: on without the tracking block, off with it. Install
    # and upgrade always wait for them.
    finalTracking: false
    podLogs: false                  # has no effect yet

  # Validating rendered resources against Kubernetes JSON schemas.
  validation:
    noResourceValidation: false
    noValuesSchemaValidation: false # skip validating values against values.schema.json
    skip:                           # keys: group, version, kind, name, namespace
      - kind=CustomThing
    extraSchemas:                   # URL templates or absolute paths, preferred over embedded schemas
      - https://schemas.example.com/{{ .NormalizedKubernetesVersion }}/{{ .ResourceKind }}.json
    # Default: 48h, but nothing is cached when the validation block is set
    # without it.
    schemaCacheLifetime: 48h

  releaseStorage:
    historyLimit: 10                # revisions to keep, 0 = all; default 10, also without releaseStorage

  typescript:
    ignoreBundleJS: false           # has no effect: TypeScript charts are not rendered yet

  # Has no effect yet. Use verify of the chart source to verify signatures.
  # provenance:
  #   strategy: never
  #   keyringFrom:
  #     name: chart-keyring
  #     dataKey: key
```

## License

Apache 2.0, see [LICENSE](LICENSE).
