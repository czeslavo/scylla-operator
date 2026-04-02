package collectors

import (
	"github.com/scylladb/scylla-operator/pkg/naming"
)

// operatorNamespaces is the fixed set of operator-owned namespaces whose
// namespace-scoped resources are collected by the manifest collectors.
var operatorNamespaces = []string{
	naming.OperatorAppName,
	naming.ScyllaManagerNamespace,
	naming.ScyllaOperatorNodeTuningNamespace,
}
