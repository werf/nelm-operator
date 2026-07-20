/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message"
// +kubebuilder:printcolumn:name="Revision",type="integer",JSONPath=".status.revision"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type Release struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec ReleaseSpec `json:"spec"`

	// +optional
	Status ReleaseStatus `json:"status,omitzero"`
}

func (r Release) GetConditions() []metav1.Condition {
	return r.Status.Conditions
}

// +kubebuilder:object:root=true

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Release `json:"items"`
}

// +kubebuilder:validation:XValidation:rule="has(self.chart) != has(self.chartRef)",message="exactly one of chart or chartRef must be set"
type ReleaseSpec struct {
	// +optional
	Chart *ReleaseChart `json:"chart,omitempty"`

	// +optional
	ChartRef *ChartSourceRef `json:"chartRef,omitempty"`

	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// +kubebuilder:default="1m"
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`

	// +kubebuilder:default="30m"
	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`

	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Values *apiextensionsv1.JSON `json:"values,omitempty"`

	// +optional
	ValuesFrom []ValuesReference `json:"valuesFrom,omitempty"`

	// +optional
	NoDefaultValues bool `json:"noDefaultValues,omitempty"`

	// +optional
	SetRootContextJSON []string `json:"setRootContextJSON,omitempty"`

	// +optional
	SecretKeyFrom *SecretKeyReference `json:"secretKeyFrom,omitempty"`

	// +optional
	SecretValuesFrom []ValuesReference `json:"secretValuesFrom,omitempty"`

	// The name of the Kubernetes service account to impersonate
	// when reconciling this Release.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// +optional
	NoDefaultSecretValues bool `json:"noDefaultSecretValues,omitempty"`

	// +optional
	Install *InstallConfig `json:"install,omitempty"`

	// +optional
	Rollback *RollbackConfig `json:"rollback,omitempty"`

	// +optional
	Uninstall *UninstallConfig `json:"uninstall,omitempty"`

	// +optional
	ExtraAnnotations map[string]string `json:"extraAnnotations,omitempty"`

	// +optional
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`

	// +optional
	RuntimeAnnotations map[string]string `json:"runtimeAnnotations,omitempty"`

	// +optional
	RuntimeLabels map[string]string `json:"runtimeLabels,omitempty"`

	// +optional
	AppVersion string `json:"appVersion,omitempty"`

	// +optional
	ReleaseInfoAnnotations map[string]string `json:"releaseInfoAnnotations,omitempty"`

	// +optional
	ReleaseLabels map[string]string `json:"releaseLabels,omitempty"`

	// +optional
	Tracking *TrackingConfig `json:"tracking,omitempty"`

	// +optional
	Provenance *ProvenanceConfig `json:"provenance,omitempty"`

	// +optional
	ReleaseStorage *ReleaseStorageConfig `json:"releaseStorage,omitempty"`

	// +optional
	Validation *ValidationConfig `json:"validation,omitempty"`

	// +optional
	Typescript *TypescriptConfig `json:"typescript,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.bucket) ? 1 : 0) + (has(self.git) ? 1 : 0) + (has(self.oci) ? 1 : 0) + (has(self.repo) ? 1 : 0) == 1", message="You must specify exactly one of: bucket, git, oci, or repo"
type ReleaseChart struct {
	// BucketChartSource defines Helm Chart from S3 bucket.
	// +optional
	BucketChartSource *BucketChartSource `json:"bucket,omitempty"`

	// GitRepositoryChartSource defines Helm Chart from Git repository.
	// +optional
	GitRepositoryChartSource *GitRepositoryChartSource `json:"git,omitempty"`

	// OCIRepositoryChartSource defines Helm Chart from OCI repository/registry.
	// +optional
	OCIRepositoryChartSource *OCIRepositoryChartSource `json:"oci,omitempty"`

	// HelmRepositoryChartSource defines Helm Chart from Helm repository.
	// +optional
	HelmRepositoryChartSource *HelmRepositoryChartSource `json:"repo,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.branch) ? 1 : 0) + (has(self.tag) ? 1 : 0) + (has(self.semver) ? 1 : 0) + (has(self.commit) ? 1 : 0) + (has(self.ref) ? 1 : 0) == 1", message="You must specify exactly one of: branch, tag, semver, commit or ref."
type GitRepositoryChartSource struct {
	// URL specifies the Git repository URL, it can be an HTTP/S or SSH address.
	// +kubebuilder:validation:Pattern="^(http|https|ssh)://.*$"
	// +required
	URL string `json:"url"`

	// Branch to check out, defaults to 'master' if no other field is defined.
	// +optional
	Branch string `json:"branch,omitempty"`

	// Tag to check out, takes precedence over Branch.
	// +optional
	Tag string `json:"tag,omitempty"`

	// SemVer tag expression to check out, takes precedence over Tag.
	// +optional
	SemVer string `json:"semver,omitempty"`

	// Commit SHA to check out, takes precedence over all reference fields.
	//
	// This can be combined with Branch to shallow clone the branch, in which
	// the commit is expected to exist.
	// +optional
	Commit string `json:"commit,omitempty"`

	// Reference is the reference to check out; takes precedence over Branch, Tag and SemVer.
	//
	// It must be a valid Git reference: https://git-scm.com/docs/git-check-ref-format#_description
	// Examples: "refs/heads/main", "refs/tags/v0.1.0", "refs/pull/420/head", "refs/merge-requests/1/head"
	// +optional
	Reference string `json:"ref,omitempty"`

	// Path specifies path to the Helm Chart in the Git repository.
	// +required
	Path string `json:"path"`

	// Alternative list of values files to use as the chart values (values.yaml
	// is not included by default), expected to be a relative path in the SourceRef.
	// Values files are merged in the order of this list with the last file overriding
	// the first. Ignored when omitted.
	// +optional
	ValuesFiles []string `json:"valuesFiles,omitempty"`

	// TODO: double check if we support such case with nelm.
	// IgnoreMissingValuesFiles controls whether to silently ignore missing values files rather than failing.
	// +optional
	IgnoreMissingValuesFiles bool `json:"ignoreMissingValuesFiles,omitempty"`

	// CredentialsFrom specifies the Secret containing authentication credentials for
	// the GitRepository.
	// For HTTPS repositories the Secret must contain 'username' and 'password'
	// fields for basic auth or 'bearerToken' field for token auth.
	// For SSH repositories the Secret must contain 'identity'
	// and 'known_hosts' fields.
	// +optional
	CredentialsFrom *CredentialReference `json:"credentialsFrom,omitempty"`

	// Provider used for authentication, can be 'azure', 'github', 'generic'.
	// When not specified, defaults to 'generic'.
	// +kubebuilder:validation:Enum=generic;azure;github
	// +optional
	Provider string `json:"provider,omitempty"`

	// ServiceAccountName is the name of the Kubernetes ServiceAccount used to
	// authenticate to the GitRepository. This field is only supported for 'azure' and 'aws' providers.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ProxySettingsFrom specifies the Secret containing the proxy configuration
	// to use while communicating with the Git server.
	// +optional
	ProxySettingsFrom *ProxySettingsReference `json:"proxySettingsFrom,omitempty"`

	// Submodules enables the initialization of all submodules within
	// the GitRepository as cloned from the URL, using their default settings.
	// +optional
	Submodules bool `json:"submodules,omitempty"`

	// SparseCheckout specifies a list of directories to checkout when cloning
	// the repository. If specified, only these directories are included in the
	// Artifact produced for this GitRepository.
	// +optional
	SparseCheckout []string `json:"sparseCheckout,omitempty"`

	// Ignore overrides the set of excluded patterns in the .sourceignore format
	// (which is the same as .gitignore). If not provided, a default will be used,
	// consult the documentation for your version to find out what those are.
	// +optional
	Ignore *string `json:"ignore,omitempty"`

	// Verification specifies the configuration to verify the Git commit
	// signature(s).
	// +optional
	Verification *sourcev1.GitRepositoryVerification `json:"verify,omitempty"`

	// Include specifies a list of GitRepository resources which Artifacts
	// should be included in the Artifact produced for this GitRepository.
	// +optional
	Include []sourcev1.GitRepositoryInclude `json:"include,omitempty"`

	// Interval at which the GitRepository URL is checked for updates.
	// This interval is approximate and may be subject to jitter to ensure
	// efficient use of resources.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Interval metav1.Duration `json:"interval"`

	// Timeout for Git operations like cloning, defaults to 60s.
	// +kubebuilder:default="60s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// ReconcileStrategy determines what enables the creation of a new artifact.
	// Valid values are ('ChartVersion', 'Revision').
	// See the documentation of the values for an explanation on their behavior.
	// Defaults to ChartVersion when omitted.
	// +kubebuilder:validation:Enum=ChartVersion;Revision
	// +kubebuilder:default:=ChartVersion
	// +optional
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(has(self.tag) ? 1 : 0) + (has(self.semver) ? 1 : 0) + (has(self.digest) ? 1 : 0) == 1", message="You must specify exactly one of: tag, semver or digest."
type OCIRepositoryChartSource struct {
	// URL is a reference to an OCI artifact repository hosted
	// on a remote container registry.
	// +kubebuilder:validation:Pattern="^oci://.*$"
	// +required
	URL string `json:"url"`

	// Tag is the image tag to pull, defaults to latest.
	// +optional
	Tag string `json:"tag,omitempty"`

	// SemVer is the range of tags to pull selecting the latest within
	// the range, takes precedence over Tag.
	// +optional
	SemVer string `json:"semver,omitempty"`

	// Digest is the image digest to pull, takes precedence over SemVer.
	// The value should be in the format 'sha256:<HASH>'.
	// +optional
	Digest string `json:"digest,omitempty"`

	// SemverFilter is a regex pattern to filter the tags within the SemVer range.
	// +optional
	SemverFilter string `json:"semverFilter,omitempty"`

	// CredentialsFrom contains the secret name containing the registry login
	// credentials to resolve image metadata.
	// The secret must be of type kubernetes.io/dockerconfigjson.
	// +optional
	CredentialsFrom *CredentialReference `json:"credentialsFrom,omitempty"`

	// CertificateFrom can be given the name of a Secret containing
	// either or both of
	//
	// - a PEM-encoded client certificate (`tls.crt`) and private
	// key (`tls.key`);
	// - a PEM-encoded CA certificate (`ca.crt`)
	//
	// and whichever are supplied, will be used for connecting to the
	// registry. The client cert and key are useful if you are
	// authenticating with a certificate; the CA cert is useful if
	// you are using a self-signed server certificate. The Secret must
	// be of type `Opaque` or `kubernetes.io/tls`.
	// +optional
	CertificateFrom *CertificateReference `json:"certificateFrom,omitempty"`

	// ProxySettingsFrom specifies the Secret containing the proxy configuration
	// to use while communicating with the container registry.
	// +optional
	ProxySettingsFrom *ProxySettingsReference `json:"proxySettingsFrom,omitempty"`

	// ServiceAccountName is the name of the Kubernetes ServiceAccount used to authenticate
	// the image pull if the service account has attached pull secrets. For more information:
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// The provider used for authentication, can be 'aws', 'azure', 'gcp' or 'generic'.
	// When not specified, defaults to 'generic'.
	// +kubebuilder:validation:Enum=generic;aws;azure;gcp
	// +kubebuilder:default:=generic
	// +optional
	Provider string `json:"provider,omitempty"`

	// Insecure allows connecting to a non-TLS HTTP container registry.
	// +optional
	Insecure bool `json:"insecure,omitempty"`

	// Ignore overrides the set of excluded patterns in the .sourceignore format
	// (which is the same as .gitignore). If not provided, a default will be used,
	// consult the documentation for your version to find out what those are.
	// +optional
	Ignore *string `json:"ignore,omitempty"`

	// LayerSelector specifies which layer should be extracted from the OCI artifact.
	// When not specified, the first layer found in the artifact is selected.
	// +optional
	LayerSelector *sourcev1.OCILayerSelector `json:"layerSelector,omitempty"`

	// Verify contains the secret name containing the trusted public keys
	// used to verify the signature and specifies which provider to use to check
	// whether OCI image is authentic.
	// +optional
	Verify *sourcev1.OCIRepositoryVerification `json:"verify,omitempty"`

	// Interval at which the OCIRepository URL is checked for updates.
	// This interval is approximate and may be subject to jitter to ensure
	// efficient use of resources.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Interval metav1.Duration `json:"interval"`

	// The timeout for remote OCI Repository operations like pulling, defaults to 60s.
	// +kubebuilder:default="60s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

type HelmRepositoryChartSource struct {
	// URL of the Helm repository, a valid URL contains at least a protocol and
	// host.
	// +kubebuilder:validation:Pattern="^(http|https|oci)://.*$"
	// +required
	URL string `json:"url"`

	// Name is the name of the Helm Chart in the specified Helm Repository.
	// +required
	Name string `json:"name"`

	// Version is the chart version semver expression.
	// Defaults to latest when omitted.
	// +optional
	Version string `json:"version,omitempty"`

	// Alternative list of values files to use as the chart values (values.yaml
	// is not included by default), expected to be a relative path in the SourceRef.
	// Values files are merged in the order of this list with the last file overriding
	// the first. Ignored when omitted.
	// +optional
	ValuesFiles []string `json:"valuesFiles,omitempty"`

	// TODO: double check if we support such case with nelm.
	// IgnoreMissingValuesFiles controls whether to silently ignore missing values files rather than failing.
	// +optional
	IgnoreMissingValuesFiles bool `json:"ignoreMissingValuesFiles,omitempty"`

	// CredentialsFrom specifies the Secret containing authentication credentials
	// for the HelmRepository.
	// For HTTP/S basic auth the secret must contain 'username' and 'password'
	// fields.
	// Support for TLS auth using the 'certFile' and 'keyFile', and/or 'caFile'
	// keys is deprecated. Please use `.spec.certSecretRef` instead.
	// +optional
	CredentialsFrom *CredentialReference `json:"credentialsFrom,omitempty"`

	// CertificateFrom can be given the name of a Secret containing
	// either or both of
	//
	// - a PEM-encoded client certificate (`tls.crt`) and private
	// key (`tls.key`);
	// - a PEM-encoded CA certificate (`ca.crt`)
	//
	// and whichever are supplied, will be used for connecting to the
	// registry. The client cert and key are useful if you are
	// authenticating with a certificate; the CA cert is useful if
	// you are using a self-signed server certificate. The Secret must
	// be of type `Opaque` or `kubernetes.io/tls`.
	//
	// It takes precedence over the values specified in the Secret referred
	// to by `.spec.secretRef`.
	// +optional
	CertificateFrom *CertificateReference `json:"certificateFrom,omitempty"`

	// PassCredentials allows the credentials from the SecretRef to be passed
	// on to a host that does not match the host as defined in URL.
	// This may be required if the host of the advertised chart URLs in the
	// index differ from the defined URL.
	// Enabling this should be done with caution, as it can potentially result
	// in credentials getting stolen in a MITM-attack.
	// +optional
	PassCredentials bool `json:"passCredentials,omitempty"`

	// Verify contains the secret name containing the trusted public keys
	// used to verify the signature and specifies which provider to use to check
	// whether OCI image is authentic.
	// This field is only supported when using HelmRepository source with spec.type 'oci'.
	// Chart dependencies, which are not bundled in the umbrella chart artifact, are not verified.
	// +optional
	Verify *sourcev1.OCIRepositoryVerification `json:"verify,omitempty"`

	// Insecure allows connecting to a non-TLS HTTP container registry.
	// This field is only taken into account if the .spec.type field is set to 'oci'.
	// +optional
	Insecure bool `json:"insecure,omitempty"`

	// Interval at which the HelmRepository URL is checked for updates.
	// This interval is approximate and may be subject to jitter to ensure
	// efficient use of resources.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`

	// Timeout is used for the index fetch operation for an HTTPS helm repository,
	// and for remote OCI Repository operations like pulling for an OCI helm
	// chart by the associated HelmChart.
	// Its default value is 60s.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

type BucketChartSource struct {
	// Endpoint is the object storage address the BucketName is located at.
	// +required
	Endpoint string `json:"endpoint"`

	// BucketName is the name of the object storage bucket.
	// +required
	BucketName string `json:"bucketName"`

	// Path specifies path to the Helm Chart in the S3 bucket.
	// +required
	Path string `json:"path"`

	// Alternative list of values files to use as the chart values (values.yaml
	// is not included by default), expected to be a relative path in the SourceRef.
	// Values files are merged in the order of this list with the last file overriding
	// the first. Ignored when omitted.
	// +optional
	ValuesFiles []string `json:"valuesFiles,omitempty"`

	// IgnoreMissingValuesFiles controls whether to silently ignore missing values files rather than failing.
	// +optional
	IgnoreMissingValuesFiles bool `json:"ignoreMissingValuesFiles,omitempty"`

	// Provider of the object storage bucket.
	// Defaults to 'generic', which expects an S3 (API) compatible object
	// storage.
	// +kubebuilder:validation:Enum=generic;aws;gcp;azure
	// +kubebuilder:default:=generic
	// +optional
	Provider string `json:"provider,omitempty"`

	// Region of the Endpoint where the BucketName is located in.
	// +optional
	Region string `json:"region,omitempty"`

	// Prefix to use for server-side filtering of files in the Bucket.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// CredentialsFrom specifies the Secret containing authentication credentials
	// for the Bucket.
	// +optional
	CredentialsFrom *CredentialReference `json:"credentialsFrom,omitempty"`

	// CertificateFrom can be given the name of a Secret containing
	// either or both of
	//
	// - a PEM-encoded client certificate (`tls.crt`) and private
	// key (`tls.key`);
	// - a PEM-encoded CA certificate (`ca.crt`)
	//
	// and whichever are supplied, will be used for connecting to the
	// bucket. The client cert and key are useful if you are
	// authenticating with a certificate; the CA cert is useful if
	// you are using a self-signed server certificate. The Secret must
	// be of type `Opaque` or `kubernetes.io/tls`.
	//
	// This field is only supported for the `generic` provider.
	// +optional
	CertificateFrom *CertificateReference `json:"certificateFrom,omitempty"`

	// ProxySettingsFrom specifies the Secret containing the proxy configuration
	// to use while communicating with the Bucket server.
	// +optional
	ProxySettingsFrom *ProxySettingsReference `json:"proxySettingsFrom,omitempty"`

	// ServiceAccountName is the name of the Kubernetes ServiceAccount used to authenticate
	// the bucket. This field is only supported for the 'gcp' and 'aws' providers.
	// For more information about workload identity:
	// https://fluxcd.io/flux/components/source/buckets/#workload-identity
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Insecure allows connecting to a non-TLS HTTP Endpoint.
	// +optional
	Insecure bool `json:"insecure,omitempty"`

	// Ignore overrides the set of excluded patterns in the .sourceignore format
	// (which is the same as .gitignore). If not provided, a default will be used,
	// consult the documentation for your version to find out what those are.
	// +optional
	Ignore *string `json:"ignore,omitempty"`

	// STS specifies the required configuration to use a Security Token
	// Service for fetching temporary credentials to authenticate in a
	// Bucket provider.
	//
	// This field is only supported for the `aws` and `generic` providers.
	// +optional
	STS *sourcev1.BucketSTSSpec `json:"sts,omitempty"`

	// Interval at which the Bucket Endpoint is checked for updates.
	// This interval is approximate and may be subject to jitter to ensure
	// efficient use of resources.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +required
	Interval metav1.Duration `json:"interval"`

	// Timeout for fetch operations, defaults to 60s.
	// +kubebuilder:default="60s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// ReconcileStrategy determines what enables the creation of a new artifact.
	// Valid values are ('ChartVersion', 'Revision').
	// See the documentation of the values for an explanation on their behavior.
	// Defaults to ChartVersion when omitted.
	// +kubebuilder:validation:Enum=ChartVersion;Revision
	// +kubebuilder:default:=ChartVersion
	// +optional
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`
}

type ChartSourceRef struct {
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// +kubebuilder:validation:Enum=HelmChart;OCIRepository
	// +required
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// +optional
	Namespace string `json:"namespace,omitempty"`
}

type ValuesReference struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	// +required
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// +kubebuilder:default="values.yaml"
	// +optional
	DataKey string `json:"dataKey,omitempty"`

	// +optional
	Optional bool `json:"optional,omitempty"`
}

type CredentialReference struct {
	// +kubebuilder:validation:Enum=Secret
	// +kubebuilder:default="Secret"
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
}

type ProxySettingsReference struct {
	// +kubebuilder:validation:Enum=Secret
	// +kubebuilder:default="Secret"
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
}

type CertificateReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
}

type SecretKeyReference struct {
	// +kubebuilder:validation:Enum=Secret
	// +kubebuilder:default="Secret"
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`

	// +kubebuilder:default="key"
	// +optional
	DataKey string `json:"dataKey,omitempty"`
}

type InstallConfig struct {
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// +optional
	AutoRollback bool `json:"autoRollback,omitempty"`

	// +optional
	NoInstallCRDs bool `json:"noInstallCRDs,omitempty"`

	// +kubebuilder:default="Background"
	// +kubebuilder:validation:Enum=Background;Foreground;Orphan
	// +optional
	DeletePropagation string `json:"deletePropagation,omitempty"`

	// +optional
	NoForceAdoption bool `json:"noForceAdoption,omitempty"`

	// +optional
	NoRemoveManualChanges bool `json:"noRemoveManualChanges,omitempty"`

	// +optional
	TemplatesAllowDNS bool `json:"templatesAllowDNS,omitempty"`

	// +optional
	Retries int `json:"retries,omitempty"`
}

type RollbackConfig struct {
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// +kubebuilder:default="Background"
	// +kubebuilder:validation:Enum=Background;Foreground;Orphan
	// +optional
	DeletePropagation string `json:"deletePropagation,omitempty"`

	// +optional
	NoForceAdoption bool `json:"noForceAdoption,omitempty"`

	// +optional
	NoRemoveManualChanges bool `json:"noRemoveManualChanges,omitempty"`
}

type UninstallConfig struct {
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// +optional
	DeleteNamespace bool `json:"deleteNamespace,omitempty"`

	// +kubebuilder:default="Background"
	// +kubebuilder:validation:Enum=Background;Foreground;Orphan
	// +optional
	DeletePropagation string `json:"deletePropagation,omitempty"`

	// +optional
	NoRemoveManualChanges bool `json:"noRemoveManualChanges,omitempty"`
}

type TrackingConfig struct {
	// +optional
	ReadinessTimeout metav1.Duration `json:"readinessTimeout,omitempty"`

	// +optional
	CreationTimeout metav1.Duration `json:"creationTimeout,omitempty"`

	// +optional
	DeletionTimeout metav1.Duration `json:"deletionTimeout,omitempty"`

	// +optional
	PodLogs bool `json:"podLogs,omitempty"`

	// +optional
	FinalTracking bool `json:"finalTracking,omitempty"`
}

type ProvenanceConfig struct {
	// +kubebuilder:default="never"
	// +kubebuilder:validation:Enum=never;digest;signature
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// FIXME: should have dedicated type.
	// +optional
	KeyringFrom *SecretKeyReference `json:"keyringFrom,omitempty"`
}

type ReleaseStorageConfig struct {
	// +kubebuilder:default=10
	// +optional
	HistoryLimit int `json:"historyLimit,omitempty"`
}

type ValidationConfig struct {
	// +optional
	NoResourceValidation bool `json:"noResourceValidation,omitempty"`

	// +optional
	NoValuesSchemaValidation bool `json:"noValuesSchemaValidation,omitempty"`

	// +optional
	LocalOnly bool `json:"localOnly,omitempty"`

	// +optional
	KubeVersion string `json:"kubeVersion,omitempty"`

	// +optional
	Skip []string `json:"skip,omitempty"`

	// +optional
	Schemas []string `json:"schemas,omitempty"`

	// +optional
	ExtraSchemas []string `json:"extraSchemas,omitempty"`

	// +optional
	SchemaCacheLifetime metav1.Duration `json:"schemaCacheLifetime,omitempty"`
}

type TypescriptConfig struct {
	// +optional
	IgnoreBundleJS bool `json:"ignoreBundleJS,omitempty"`
}

type ReleaseStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Revision int `json:"revision,omitempty"`

	// +optional
	RevisionStatus string `json:"revisionStatus,omitempty"`

	// +optional
	LastAction string `json:"lastAction,omitempty"`

	// +optional
	LastActionFailures int `json:"lastActionFailures,omitempty"`

	// NOTE: required by future indexer based solution. Should be papulater on chart retrival.

	// LastAttemptedArtifactRevision is the Source revision of the last reconciliation
	// attempt. For OCIRepository sources, the 12 first characters of the digest are
	// appended to the chart version e.g. "1.2.3+1234567890ab".
	// +optional
	LastAttemptedArtifactRevision string `json:"lastAttemptedArtifactRevision,omitempty"`

	// LastAttemptedArtifactDigest is the digest of the last reconciliation attempt.
	// This is only set for OCIRepository sources.
	// +optional
	LastAttemptedArtifactDigest string `json:"lastAttemptedArtifactDigest,omitempty"`

	// NOTE: required by idempotent and atomic shared repository management.

	// +optional
	ChartSourcePhase string `json:"chartSourcePhase,omitempty"`
	// +optional
	LastAppliedChartSource *ChartSourceReference `json:"lastAppliedChartSource,omitempty"`
	// +optional
	CandidateChartSource *ChartSourceReference `json:"candidateChartSource,omitempty"`
}

type ChartSourceReference struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

func (g *ChartSourceReference) GroupVersionKind() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   g.Group,
		Version: g.Version,
		Kind:    g.Kind,
	}
}

func (r *Release) GetInstallTimeout() time.Duration {
	if r.Spec.Install != nil && r.Spec.Install.Timeout != nil {
		return r.Spec.Install.Timeout.Duration
	}
	return r.Spec.Timeout.Duration
}

func (r *Release) GetRollbackTimeout() time.Duration {
	if r.Spec.Rollback != nil && r.Spec.Rollback.Timeout != nil {
		return r.Spec.Rollback.Timeout.Duration
	}
	return r.Spec.Timeout.Duration
}

func (r *Release) GetUninstallTimeout() time.Duration {
	if r.Spec.Uninstall != nil && r.Spec.Uninstall.Timeout != nil {
		return r.Spec.Uninstall.Timeout.Duration
	}
	return r.Spec.Timeout.Duration
}

func (r *Release) GetReleaseNamespace() string {
	if r.Spec.TargetNamespace != "" {
		return r.Spec.TargetNamespace
	}
	return r.Namespace
}

func init() {
	SchemeBuilder.Register(&Release{}, &ReleaseList{})
}
