package values

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nelmv1alpha1 "github.com/werf/nelm-operator/api/v1alpha1"
)

type ResolvedValues struct {
	ValuesFiles           []string
	SecretValuesFiles     []string
	SecretKey             string
	ProvenanceKeyringPath string
}

func Resolve(ctx context.Context, c client.Client, rel *nelmv1alpha1.Release, chartValuesFiles []string, tempDir string) (*ResolvedValues, error) {
	result := &ResolvedValues{}

	if chartValuesFiles != nil {
		result.ValuesFiles = chartValuesFiles
	}

	if rel.Spec.Values != nil {
		path, err := writeToTempFile(rel.Spec.Values.Raw, tempDir, "inline-values-*.yaml")
		if err != nil {
			return nil, fmt.Errorf("write inline values: %w", err)
		}
		result.ValuesFiles = append(result.ValuesFiles, path)
	}

	for i, ref := range rel.Spec.ValuesFrom {
		data, err := fetchReference(ctx, c, rel.Namespace, ref)
		if err != nil {
			if ref.Optional {
				continue
			}
			return nil, fmt.Errorf("resolve valuesFrom[%d] %s/%s: %w", i, ref.Kind, ref.Name, err)
		}

		path, err := writeToTempFile(data, tempDir, fmt.Sprintf("values-from-%d-*.yaml", i))
		if err != nil {
			return nil, fmt.Errorf("write valuesFrom[%d]: %w", i, err)
		}
		result.ValuesFiles = append(result.ValuesFiles, path)
	}

	if rel.Spec.SecretKeyFrom != nil {
		key, err := fetchSecretKey(ctx, c, rel.Namespace, rel.Spec.SecretKeyFrom)
		if err != nil {
			return nil, fmt.Errorf("resolve secretKeyFrom: %w", err)
		}
		result.SecretKey = key
	}

	for i, ref := range rel.Spec.SecretValuesFrom {
		data, err := fetchReference(ctx, c, rel.Namespace, ref)
		if err != nil {
			if ref.Optional {
				continue
			}
			return nil, fmt.Errorf("resolve secretValuesFrom[%d] %s/%s: %w", i, ref.Kind, ref.Name, err)
		}

		path, err := writeToTempFile(data, tempDir, fmt.Sprintf("secret-values-%d-*.yaml", i))
		if err != nil {
			return nil, fmt.Errorf("write secretValuesFrom[%d]: %w", i, err)
		}
		result.SecretValuesFiles = append(result.SecretValuesFiles, path)
	}

	if rel.Spec.Provenance != nil && rel.Spec.Provenance.KeyringFrom != nil {
		keyringDataKey := rel.Spec.Provenance.KeyringFrom.DataKey
		if keyringDataKey == "" {
			keyringDataKey = "key"
		}
		data, err := fetchSecretData(ctx, c, rel.Namespace, rel.Spec.Provenance.KeyringFrom.Name, keyringDataKey)
		if err != nil {
			return nil, fmt.Errorf("resolve provenance keyring: %w", err)
		}

		path, err := writeToTempFile(data, tempDir, "provenance-keyring-*")
		if err != nil {
			return nil, fmt.Errorf("write provenance keyring: %w", err)
		}
		result.ProvenanceKeyringPath = path
	}

	return result, nil
}

func writeToTempFile(data []byte, tempDir string, pattern string) (string, error) {
	f, err := os.CreateTemp(tempDir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file %s: %w", pattern, err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", fmt.Errorf("write temp file %s: %w", pattern, err)
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temp file %s: %w", pattern, err)
	}

	return f.Name(), nil
}

func fetchReference(ctx context.Context, c client.Client, namespace string, ref nelmv1alpha1.ValuesReference) ([]byte, error) {
	switch ref.Kind {
	case "ConfigMap":
		return fetchConfigMapData(ctx, c, namespace, ref.Name, ref.DataKey)
	case "Secret":
		return fetchSecretData(ctx, c, namespace, ref.Name, ref.DataKey)
	default:
		return nil, fmt.Errorf("unsupported kind %q", ref.Kind)
	}
}

func fetchConfigMapData(ctx context.Context, c client.Client, namespace, name, key string) ([]byte, error) {
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cm); err != nil {
		return nil, err
	}

	if key == "" {
		key = "values.yaml"
	}

	data, ok := cm.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in ConfigMap %s/%s", key, namespace, name)
	}
	return []byte(data), nil
}

func fetchSecretData(ctx context.Context, c client.Client, namespace, name, key string) ([]byte, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &secret); err != nil {
		return nil, err
	}

	if key == "" {
		key = "values.yaml"
	}

	data, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in Secret %s/%s", key, namespace, name)
	}
	return data, nil
}

func fetchSecretKey(ctx context.Context, c client.Client, namespace string, ref *nelmv1alpha1.SecretKeyReference) (string, error) {
	key := ref.DataKey
	if key == "" {
		key = "key"
	}

	data, err := fetchSecretData(ctx, c, namespace, ref.Name, key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
