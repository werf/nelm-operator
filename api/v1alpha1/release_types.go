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

	// +kubebuilder:default=true
	// +optional
	DefaultValues *bool `json:"defaultValues,omitempty"`

	// +optional
	SetRootContextJSON []string `json:"setRootContextJSON,omitempty"`

	// +optional
	SecretKeyFrom *SecretKeyReference `json:"secretKeyFrom,omitempty"`

	// +optional
	SecretValuesFrom []ValuesReference `json:"secretValuesFrom,omitempty"`

	// +kubebuilder:default=true
	// +optional
	DefaultSecretValues *bool `json:"defaultSecretValues,omitempty"`

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

type ReleaseChart struct {
	Name string `json:"name"`

	// +optional
	Version string `json:"version,omitempty"`

	SourceRef CrossNamespaceObjectReference `json:"sourceRef"`

	// +kubebuilder:default="1m"
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`

	// +kubebuilder:default="ChartVersion"
	// +kubebuilder:validation:Enum=ChartVersion;Revision
	// +optional
	ReconcileStrategy string `json:"reconcileStrategy,omitempty"`

	// +optional
	Verify *ChartVerification `json:"verify,omitempty"`
}

type CrossNamespaceObjectReference struct {
	// +kubebuilder:validation:Enum=HelmRepository;GitRepository;Bucket
	Kind string `json:"kind"`

	Name string `json:"name"`

	// +optional
	Namespace string `json:"namespace,omitempty"`
}

type ChartSourceRef struct {
	// +kubebuilder:validation:Enum=HelmChart;OCIRepository
	Kind string `json:"kind"`

	Name string `json:"name"`

	// +optional
	Namespace string `json:"namespace,omitempty"`
}

type ChartVerification struct {
	// +kubebuilder:validation:Enum=cosign
	Provider string `json:"provider"`

	// +optional
	SecretRef *LocalObjectReference `json:"secretRef,omitempty"`
}

type LocalObjectReference struct {
	Name string `json:"name"`
}

type ValuesReference struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Kind string `json:"kind"`

	Name string `json:"name"`

	// +kubebuilder:default="values.yaml"
	// +optional
	DataKey string `json:"dataKey,omitempty"`

	// +optional
	Optional bool `json:"optional,omitempty"`
}

type SecretKeyReference struct {
	// +kubebuilder:validation:Enum=Secret
	// +kubebuilder:default="Secret"
	Kind string `json:"kind"`

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

	// +kubebuilder:default=true
	// +optional
	InstallCRDs *bool `json:"installCRDs,omitempty"`

	// +kubebuilder:default="Background"
	// +kubebuilder:validation:Enum=Background;Foreground;Orphan
	// +optional
	DeletePropagation string `json:"deletePropagation,omitempty"`

	// +kubebuilder:default=true
	// +optional
	ForceAdoption *bool `json:"forceAdoption,omitempty"`

	// +kubebuilder:default=true
	// +optional
	RemoveManualChanges *bool `json:"removeManualChanges,omitempty"`

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

	// +kubebuilder:default=true
	// +optional
	ForceAdoption *bool `json:"forceAdoption,omitempty"`

	// +kubebuilder:default=true
	// +optional
	RemoveManualChanges *bool `json:"removeManualChanges,omitempty"`
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

	// +kubebuilder:default=true
	// +optional
	RemoveManualChanges *bool `json:"removeManualChanges,omitempty"`
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

	// +optional
	KeyringFrom *SecretKeyReference `json:"keyringFrom,omitempty"`
}

type ReleaseStorageConfig struct {
	// +kubebuilder:default=10
	// +optional
	HistoryLimit int `json:"historyLimit,omitempty"`
}

type ValidationConfig struct {
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// +kubebuilder:default=true
	// +optional
	ValuesSchemaValidation *bool `json:"valuesSchemaValidation,omitempty"`

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
