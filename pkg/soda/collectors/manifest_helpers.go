package collectors

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// operatorNamespaces is the fixed set of operator-owned namespaces whose
// namespace-scoped resources are collected by the manifest collectors.
var operatorNamespaces = []string{
	naming.OperatorAppName,
	naming.ScyllaManagerNamespace,
	naming.ScyllaOperatorNodeTuningNamespace,
}

// marshalObjectYAML serializes a Kubernetes runtime.Object to YAML.
// The object's managed fields are cleared before serialization to reduce noise.
func marshalObjectYAML(obj runtime.Object) ([]byte, error) {
	// Use sigs.k8s.io/yaml which round-trips through JSON for proper field ordering.
	data, err := yaml.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshaling object to YAML: %w", err)
	}
	return data, nil
}
