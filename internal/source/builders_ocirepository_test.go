package source

import (
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

func TestBuildChartSourceFromReleaseOCILayerSelector(t *testing.T) {
	ignore := "*.md"

	tests := []struct {
		name          string
		ignore        *string
		layerSelector *sourcev1.OCILayerSelector
		want          sourcev1.OCILayerSelector
	}{
		{
			name: "unset selects the Helm chart layer and copies it",
			want: sourcev1.OCILayerSelector{
				MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				Operation: sourcev1.OCILayerCopy,
			},
		},
		{
			name:   "unset with ignore selects the Helm chart layer and extracts it",
			ignore: &ignore,
			want: sourcev1.OCILayerSelector{
				MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
				Operation: sourcev1.OCILayerExtract,
			},
		},
		{
			name: "operation only keeps the media type empty",
			layerSelector: &sourcev1.OCILayerSelector{
				Operation: sourcev1.OCILayerExtract,
			},
			want: sourcev1.OCILayerSelector{
				Operation: sourcev1.OCILayerExtract,
			},
		},
		{
			name: "set is kept as is",
			layerSelector: &sourcev1.OCILayerSelector{
				MediaType: "application/vnd.cncf.flux.content.v1.tar+gzip",
				Operation: sourcev1.OCILayerExtract,
			},
			want: sourcev1.OCILayerSelector{
				MediaType: "application/vnd.cncf.flux.content.v1.tar+gzip",
				Operation: sourcev1.OCILayerExtract,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &nelmv1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{Name: "podinfo", Namespace: "default"},
				Spec: nelmv1alpha1.ReleaseSpec{
					Chart: &nelmv1alpha1.ReleaseChart{
						OCIRepositoryChartSource: &nelmv1alpha1.OCIRepositoryChartSource{
							URL:           "oci://ghcr.io/stefanprodan/charts/podinfo",
							Ignore:        tt.ignore,
							LayerSelector: tt.layerSelector,
						},
					},
				},
			}

			origRel := rel.DeepCopy()

			obj, err := BuildChartSourceFromRelease(sourcev1.GroupVersion.Group, sourcev1.GroupVersion.Version, rel)
			if err != nil {
				t.Fatalf("BuildChartSourceFromRelease() error = %v", err)
			}

			repo, ok := obj.(*sourcev1.OCIRepository)
			if !ok {
				t.Fatalf("BuildChartSourceFromRelease() = %T, want *sourcev1.OCIRepository", obj)
			}

			if repo.Spec.LayerSelector == nil || *repo.Spec.LayerSelector != tt.want {
				t.Errorf("Spec.LayerSelector = %+v, want %+v", repo.Spec.LayerSelector, tt.want)
			}

			if repo.Spec.Ignore != tt.ignore {
				t.Errorf("Spec.Ignore = %v, want %v", repo.Spec.Ignore, tt.ignore)
			}

			if !equality.Semantic.DeepEqual(rel, origRel) {
				t.Errorf("Release was modified")
			}
		})
	}
}
