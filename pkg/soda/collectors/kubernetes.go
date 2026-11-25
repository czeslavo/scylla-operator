package collectors

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/build"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
)

// SODA's own collectors. Their entries use Scylla Doctor's vitals format and live in a separate per-node vitals file
// (soda/vitals/<node>.vitals.json): Scylla Doctor's tools reject vitals without their SDVersionCollector, and
// Scylla Doctor's own files stay byte-identical in the archive.
const (
	SODAVersionCollector             = "SODAVersionCollector"
	ScyllaDBRuntimeContractCollector = "ScyllaDBRuntimeContractCollector"
)

func passed(data any) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorPassed, Data: data, Message: "Data collected", Mask: []any{}}
}

func failed(message string) vitals.CollectorResult {
	return vitals.CollectorResult{Status: vitals.CollectorFailed, Data: map[string]any{}, Message: message, Mask: []any{}}
}

// VersionResult identifies the SODA build in every SODA vitals file.
func VersionResult() vitals.CollectorResult {
	return passed(map[string]any{
		"version":       vitals.SODAVersion(build.GitCommit()),
		"formatVersion": "soda.scylladb.com/vitals/v1alpha1",
	})
}

// SODAVitals builds a node's SODA vitals from what CollectScyllaDoctor gathered; Kubernetes collectors add theirs.
func SODAVitals(out *NodeOutput, c *contract.Contract) vitals.Vitals {
	v := vitals.Vitals{}
	v[SODAVersionCollector] = VersionResult()

	switch {
	case len(out.ContractError) == 0 && len(out.ContractResults) == 0:
		v[ScyllaDBRuntimeContractCollector] = vitals.CollectorResult{Status: vitals.CollectorSkipped, Data: map[string]any{},
			Message: "The runtime contract check didn't run: " + out.Collection.Reason, Mask: []any{}}
	case len(out.ContractError) > 0:
		v[ScyllaDBRuntimeContractCollector] = failed(fmt.Sprintf("can't run the runtime contract check: %s", out.ContractError))
	default:
		r := passed(map[string]any{
			"schemaVersion":       c.SchemaVersion,
			"scyllaDoctorVersion": c.ScyllaDoctorVersion,
			"interpreter":         c.Interpreter,
			"results":             out.ContractResults,
		})
		v[ScyllaDBRuntimeContractCollector] = r
	}
	return v
}
