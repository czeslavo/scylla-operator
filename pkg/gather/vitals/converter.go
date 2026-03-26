package vitals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

// ConversionResult holds metadata about one pod's vitals conversion.
type ConversionResult struct {
	PodName        string
	OutputPath     string
	CollectorCount map[string]bool
}

// Converter converts must-gather artifacts into Scylla Doctor vitals JSON.
type Converter struct {
	mustGatherDir string
	outputDir     string
}

// NewConverter creates a new Converter.
func NewConverter(mustGatherDir, outputDir string) *Converter {
	return &Converter{
		mustGatherDir: mustGatherDir,
		outputDir:     outputDir,
	}
}

// artifactMapping maps must-gather artifact filenames to their parser functions
// and the Scylla Doctor collector name they produce.
type artifactMapping struct {
	Filename      string
	CollectorName string
	Parser        func(string) (CollectorResult, error)
}

// artifactMappings defines all the artifact-to-collector mappings.
var artifactMappings = []artifactMapping{
	{
		Filename:      "uname.log",
		CollectorName: "ComputerArchitectureCollector",
		Parser:        parseUname,
	},
	{
		Filename:      "os-release.log",
		CollectorName: "OSCollector",
		Parser:        parseOsRelease,
	},
	{
		Filename:      "cpuinfo.log",
		CollectorName: "CPUSpecificationsCollector",
		Parser:        parseLscpu,
	},
	{
		Filename:      "free.log",
		CollectorName: "RAMCollector",
		Parser:        parseFree,
	},
	{
		Filename:      "scylla-version.log",
		CollectorName: "ScyllaVersionCollector",
		Parser:        parseScyllaVersion,
	},
	{
		Filename:      "scylla.d-contents.log",
		CollectorName: "ScyllaExtraConfigurationFilesCollector",
		Parser:        parseScyllaDContents,
	},
	{
		Filename:      "scylla-api-schema-versions.log",
		CollectorName: "ScyllaClusterSchemaCollector",
		Parser:        parseSchemaVersions,
	},
}

// Convert walks the must-gather directory, finds pod artifact directories,
// parses collected files, and writes vitals.json per pod.
//
// must-gather directory structure:
//
//	<must-gather-dir>/
//	  namespaces/
//	    <namespace>/
//	      pods/
//	        <pod-name>/
//	          uname.log
//	          os-release.log
//	          lscpu.log
//	          free.log
//	          scylla-version.log
//	          scylla.d-contents.log
//	          scylla-api-schema-versions.log
func (c *Converter) Convert() ([]ConversionResult, error) {
	podDirs, err := c.findPodDirectories()
	if err != nil {
		return nil, fmt.Errorf("can't find pod directories: %w", err)
	}

	if len(podDirs) == 0 {
		klog.InfoS("No pod directories found with diagnostic artifacts", "MustGatherDir", c.mustGatherDir)
		return nil, nil
	}

	var results []ConversionResult
	for _, pd := range podDirs {
		result, err := c.convertPod(pd)
		if err != nil {
			klog.ErrorS(err, "Failed to convert pod artifacts", "PodDir", pd.path, "Pod", pd.podName)
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}

	return results, nil
}

// podDirectory holds info about a discovered pod artifact directory.
type podDirectory struct {
	path      string
	podName   string
	namespace string
}

// findPodDirectories discovers pod directories that contain at least one
// diagnostic artifact we know how to convert.
func (c *Converter) findPodDirectories() ([]podDirectory, error) {
	// Walk the must-gather directory looking for the pattern:
	// <root>/namespaces/<ns>/pods/<pod-name>/
	namespacesDir := filepath.Join(c.mustGatherDir, "namespaces")
	if _, err := os.Stat(namespacesDir); os.IsNotExist(err) {
		// Try without the "namespaces" prefix — maybe the user pointed directly
		// at the inner directory.
		klog.V(2).InfoS("No 'namespaces' subdirectory found, scanning must-gather-dir directly", "MustGatherDir", c.mustGatherDir)
		return c.findPodDirectoriesInRoot(c.mustGatherDir)
	}

	nsEntries, err := os.ReadDir(namespacesDir)
	if err != nil {
		return nil, fmt.Errorf("can't read namespaces directory %q: %w", namespacesDir, err)
	}

	var podDirs []podDirectory
	for _, nsEntry := range nsEntries {
		if !nsEntry.IsDir() {
			continue
		}

		nsName := nsEntry.Name()
		podsDir := filepath.Join(namespacesDir, nsName, "pods")
		if _, err := os.Stat(podsDir); os.IsNotExist(err) {
			continue
		}

		podEntries, err := os.ReadDir(podsDir)
		if err != nil {
			klog.ErrorS(err, "Can't read pods directory", "PodsDir", podsDir)
			continue
		}

		for _, podEntry := range podEntries {
			if !podEntry.IsDir() {
				continue
			}

			podDir := filepath.Join(podsDir, podEntry.Name())
			if c.hasAnyArtifact(podDir) {
				podDirs = append(podDirs, podDirectory{
					path:      podDir,
					podName:   podEntry.Name(),
					namespace: nsName,
				})
			}
		}
	}

	return podDirs, nil
}

// findPodDirectoriesInRoot handles the case where the must-gather-dir might
// itself be a pod directory or contain pod directories directly.
func (c *Converter) findPodDirectoriesInRoot(root string) ([]podDirectory, error) {
	// Check if root itself is a pod dir.
	if c.hasAnyArtifact(root) {
		return []podDirectory{{
			path:      root,
			podName:   filepath.Base(root),
			namespace: "unknown",
		}}, nil
	}

	// Otherwise walk one level.
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("can't read directory %q: %w", root, err)
	}

	var podDirs []podDirectory
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if c.hasAnyArtifact(dir) {
			podDirs = append(podDirs, podDirectory{
				path:      dir,
				podName:   e.Name(),
				namespace: "unknown",
			})
		}
	}

	return podDirs, nil
}

// hasAnyArtifact checks if a directory contains at least one diagnostic artifact.
func (c *Converter) hasAnyArtifact(dir string) bool {
	for _, m := range artifactMappings {
		if _, err := os.Stat(filepath.Join(dir, m.Filename)); err == nil {
			return true
		}
	}
	return false
}

// convertPod converts all artifacts in a pod directory to a vitals JSON file.
func (c *Converter) convertPod(pd podDirectory) (*ConversionResult, error) {
	vitals := make(map[string]CollectorResult)

	for _, m := range artifactMappings {
		artifactPath := filepath.Join(pd.path, m.Filename)
		content, err := os.ReadFile(artifactPath)
		if err != nil {
			if os.IsNotExist(err) {
				klog.V(2).InfoS("Artifact not found, skipping collector", "Artifact", m.Filename, "Collector", m.CollectorName, "Pod", pd.podName)
				continue
			}
			klog.ErrorS(err, "Can't read artifact, skipping", "Artifact", m.Filename, "Pod", pd.podName)
			continue
		}

		result, err := m.Parser(string(content))
		if err != nil {
			klog.ErrorS(err, "Can't parse artifact, marking as failed", "Artifact", m.Filename, "Collector", m.CollectorName, "Pod", pd.podName)
			vitals[m.CollectorName] = CollectorResult{
				Status:  StatusFailed,
				Data:    map[string]interface{}{},
				Output:  []OutputEntry{},
				Message: fmt.Sprintf("Parse error: %v", err),
				Mask:    []interface{}{},
			}
			continue
		}

		vitals[m.CollectorName] = result
	}

	// Add synthetic SystemConfigCollector entry if we have schema versions data.
	// ScyllaClusterSchemaCollector depends on SystemConfigCollector for api_address/api_port.
	if _, hasSchema := vitals["ScyllaClusterSchemaCollector"]; hasSchema {
		vitals["SystemConfigCollector"] = newSyntheticSystemConfigResult()
	}

	if len(vitals) == 0 {
		klog.V(2).InfoS("No artifacts converted for pod", "Pod", pd.podName)
		return nil, nil
	}

	// Fill in SKIPPED entries for all collectors we don't produce, so that
	// dependent analyzers in Scylla Doctor cascade to SKIPPED instead of
	// FAILED with "results not found".
	fillSkippedCollectors(vitals)

	// Determine output path.
	outputDir := c.outputDir
	if pd.namespace != "unknown" {
		outputDir = filepath.Join(c.outputDir, "namespaces", pd.namespace, "pods", pd.podName)
	} else {
		outputDir = filepath.Join(c.outputDir, pd.podName)
	}

	if err := os.MkdirAll(outputDir, 0770); err != nil {
		return nil, fmt.Errorf("can't create output directory %q: %w", outputDir, err)
	}

	outputPath := filepath.Join(outputDir, "vitals.json")
	jsonData, err := json.MarshalIndent(vitals, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("can't marshal vitals JSON: %w", err)
	}

	if err := os.WriteFile(outputPath, jsonData, 0666); err != nil {
		return nil, fmt.Errorf("can't write vitals file %q: %w", outputPath, err)
	}

	collectorCount := make(map[string]bool)
	for name, result := range vitals {
		collectorCount[name] = result.Status == StatusPassed
	}

	klog.InfoS("Converted pod artifacts to vitals",
		"Pod", pd.podName,
		"Namespace", pd.namespace,
		"Collectors", len(vitals),
		"OutputPath", outputPath,
	)

	return &ConversionResult{
		PodName:        pd.podName,
		OutputPath:     outputPath,
		CollectorCount: collectorCount,
	}, nil
}

// newSyntheticSystemConfigResult creates a synthetic SystemConfigCollector entry
// with default values for api_address and api_port. This satisfies the dependency
// that ScyllaClusterSchemaCollector and other downstream analyzers have on
// SystemConfigCollector.
//
// In a Kubernetes context, the Scylla API always listens on localhost:10000
// within the pod, which is the default behavior.
func newSyntheticSystemConfigResult() CollectorResult {
	data := map[string]interface{}{
		"api_address": map[string]interface{}{
			"value":  "localhost",
			"source": "default",
			"type":   "string",
		},
		"api_port": map[string]interface{}{
			"value":  10000,
			"source": "default",
			"type":   "int",
		},
		"listen_address": map[string]interface{}{
			"value":  "0.0.0.0",
			"source": "default",
			"type":   "string",
		},
		"broadcast_address": map[string]interface{}{
			"value":  "0.0.0.0",
			"source": "default",
			"type":   "string",
		},
		"rpc_address": map[string]interface{}{
			"value":  "localhost",
			"source": "default",
			"type":   "string",
		},
		"broadcast_rpc_address": map[string]interface{}{
			"value":  "localhost",
			"source": "default",
			"type":   "string",
		},
		"workdir,W": map[string]interface{}{
			"value":  "/var/lib/scylla",
			"source": "default",
			"type":   "string",
		},
		"data_file_directories": map[string]interface{}{
			"value":  []string{"/var/lib/scylla/data"},
			"source": "default",
			"type":   "string[]",
		},
		"commitlog_directory": map[string]interface{}{
			"value":  "/var/lib/scylla/commitlog",
			"source": "default",
			"type":   "string",
		},
	}

	return NewPassedResult(data, "Synthetic entry for Kubernetes (default Scylla API config)")
}

// allScyllaDoctorCollectors is the complete list of collector class names from
// Scylla Doctor. Collectors that our converter doesn't produce data for will be
// emitted with SKIPPED status so that dependent analyzers cascade to SKIPPED
// (instead of FAILED with "results not found").
//
// This list was extracted from scylla-doctor/scylla-doctor/collectors.py and
// should be updated when new collectors are added to Scylla Doctor.
var allScyllaDoctorCollectors = []string{
	"ClientConnectionCollector",
	"ClockSourceCollector",
	"ComputerArchitectureCollector",
	"CoredumpCollector",
	"CPUScalingCollector",
	"CPUSetCollector",
	"CPUSpecificationsCollector",
	"CqlshCollector",
	"FirewallRulesCollector",
	"GossipInfoCollector",
	"HypervisorTypeCollector",
	"InfrastructureProviderCollector",
	"IPAddressesCollector",
	"IPRoutesCollector",
	"KernelRingBufferCollector",
	"LSPCICollector",
	"MaintenanceEventsCollector",
	"NICsCollector",
	"NodePlatformCollector",
	"NodetoolCFStatsCollector",
	"NTPServicesCollector",
	"NTPStatusCollector",
	"OSCollector",
	"PathsCollector",
	"PerftuneSystemConfigurationCollector",
	"PerftuneYamlDefaultCollector",
	"ProcInterruptsCollector",
	"RaftGroup0Collector",
	"RaftTopologyRPCStatusCollector",
	"RAIDSetupCollector",
	"RAMCollector",
	"RsyslogCollector",
	"ScyllaBinaryCollector",
	"ScyllaClusterSchemaCollector",
	"ScyllaClusterSchemaDescriptionCollector",
	"ScyllaClusterStatusCollector",
	"ScyllaClusterSystemKeyspacesCollector",
	"ScyllaClusterTablesDescriptionCollector",
	"ScyllaConfigurationFileCollector",
	"ScyllaConfigurationFileNoParsingCollector",
	"ScyllaExtraConfigurationFilesCollector",
	"ScyllaLimitNOFILECollector",
	"ScyllaLogsCollector",
	"ScyllaSeedsCollector",
	"ScyllaServicesCollector",
	"ScyllaSSTablesCollector",
	"ScyllaSystemConfigurationFilesCollector",
	"ScyllaTablesCompressionInfoCollector",
	"ScyllaTablesUsedDiskCollector",
	"ScyllaVersionCollector",
	"SDVersionCollector",
	"SeastarCPUMapCollector",
	"SELinuxCollector",
	"ServiceManagerCollector",
	"StorageConfigurationCollector",
	"SwapCollector",
	"SysctlCollector",
	"SystemClusterStatusCollector",
	"SystemConfigCollector",
	"SystemPeersLocalCollector",
	"SystemTopologyCollector",
	"TCPConnectionsCollector",
	"TokenMetadataHostsMappingCollector",
}

// fillSkippedCollectors adds SKIPPED entries for all known Scylla Doctor
// collectors that are not already present in the vitals map. This ensures
// that dependent analyzers cascade to SKIPPED (clean output) rather than
// FAILED with "results not found" (noisy output).
func fillSkippedCollectors(vitals map[string]CollectorResult) {
	for _, name := range allScyllaDoctorCollectors {
		if _, exists := vitals[name]; !exists {
			vitals[name] = NewSkippedResult("Not available in Kubernetes must-gather collection")
		}
	}
}

// podNameFromDir extracts a human-readable identifier from a pod directory path.
func podNameFromDir(dir string) string {
	parts := strings.Split(filepath.ToSlash(dir), "/")
	for i, p := range parts {
		if p == "pods" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return filepath.Base(dir)
}
