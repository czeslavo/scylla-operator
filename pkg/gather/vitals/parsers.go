package vitals

import (
	"bufio"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// parseUname parses the output of "uname --all" and produces data matching
// Scylla Doctor's ComputerArchitectureCollector.
//
// Example input:
//
//	Linux scylla-node-0 5.15.0-91-generic #101-Ubuntu SMP x86_64 x86_64 x86_64 GNU/Linux
//
// Expected data shape:
//
//	{"architecture": "x86_64", "kernel_version": "5.15.0-91-generic"}
func parseUname(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty uname output"), nil
	}

	fields := strings.Fields(content)
	if len(fields) < 3 {
		return CollectorResult{}, fmt.Errorf("unexpected uname output format: %q", content)
	}

	// Kernel version is the 3rd field (index 2), e.g., "5.15.0-91-generic"
	kernelVersion := fields[2]

	// Architecture is the second-to-last field
	architecture := fields[len(fields)-2]

	data := map[string]interface{}{
		"architecture":   architecture,
		"kernel_version": kernelVersion,
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// parseOsRelease parses the output of "cat /etc/os-release" and produces data
// matching Scylla Doctor's OSCollector.
//
// Example input:
//
//	ID=ubuntu
//	VERSION_ID="22.04"
//
// Expected data shape:
//
//	{"name": "ubuntu", "version": "22", "version_minor": "22.04"}
func parseOsRelease(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty os-release output"), nil
	}

	values := parseKeyValueFile(content)

	name := stripQuotes(values["ID"])
	versionID := stripQuotes(values["VERSION_ID"])

	version := versionID
	if idx := strings.Index(versionID, "."); idx >= 0 {
		version = versionID[:idx]
	}

	data := map[string]interface{}{
		"name":          name,
		"version":       version,
		"version_minor": versionID,
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// parseLscpu parses the output of "lscpu" and produces data matching
// Scylla Doctor's CPUSpecificationsCollector.
//
// Expected data shape:
//
//	{"flags": ["sse", "sse2", ...], "logical_cores": 8}
func parseLscpu(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty lscpu output"), nil
	}

	var flags []string
	var logicalCores int

	scanner := bufio.NewScanner(strings.NewReader(content))
	cpuRegex := regexp.MustCompile(`^CPU\(s\):\s+(\d+)`)
	flagsRegex := regexp.MustCompile(`^Flags:\s+(.+)`)

	for scanner.Scan() {
		line := scanner.Text()

		if m := cpuRegex.FindStringSubmatch(line); m != nil {
			v, err := strconv.Atoi(m[1])
			if err == nil {
				logicalCores = v
			}
		}

		if m := flagsRegex.FindStringSubmatch(line); m != nil {
			flags = strings.Fields(m[1])
		}
	}

	if flags == nil {
		flags = []string{}
	}

	data := map[string]interface{}{
		"flags":         flags,
		"logical_cores": logicalCores,
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// parseFree parses the output of "free" and produces data matching
// Scylla Doctor's RAMCollector.
//
// Example input:
//
//	              total        used        free      shared  buff/cache   available
//	Mem:       16384000     8192000     4096000      512000     4096000    12288000
//	Swap:       2097152      524288     1572864
//
// Expected data shape:
//
//	{"total": 16384000}
func parseFree(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty free output"), nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Mem:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				total, err := strconv.Atoi(fields[1])
				if err != nil {
					return CollectorResult{}, fmt.Errorf("can't parse RAM total %q: %w", fields[1], err)
				}

				data := map[string]interface{}{
					"total": total,
				}
				return NewPassedResult(data, "Converted from must-gather"), nil
			}
		}
	}

	return CollectorResult{}, fmt.Errorf("can't find Mem: line in free output")
}

// parseScyllaVersion parses the output of "scylla --version" and produces data
// matching Scylla Doctor's ScyllaVersionCollector.
//
// Example input:
//
//	5.4.0-0.20231113.b4f3f037c635
//
// Expected data shape:
//
//	{"version": "5.4.0-...", "edition": "oss", "packages": []}
//
// Note: In a container context we can't run rpm/dpkg to get packages and edition,
// so we default edition to "oss" and packages to empty. The ScyllaVersionCollector
// analyzer primarily uses the version string.
func parseScyllaVersion(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty scylla --version output"), nil
	}

	// Heuristic: if the version string contains "enterprise", mark it as such.
	edition := "oss"
	if strings.Contains(strings.ToLower(content), "enterprise") {
		edition = "enterprise"
	}

	data := map[string]interface{}{
		"version":  content,
		"edition":  edition,
		"packages": []string{},
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// parseScyllaDContents parses the output of the bash one-liner that dumps all
// files under /etc/scylla.d/ and produces data matching Scylla Doctor's
// ScyllaExtraConfigurationFilesCollector.
//
// Input format (from: for f in /etc/scylla.d/*; do echo "=== $f ==="; cat "$f"; done):
//
//	=== /etc/scylla.d/cpuset.conf ===
//	CPUSET="--cpuset 0,1,2,3"
//	=== /etc/scylla.d/io_properties.yaml ===
//	disks:
//	  - mountpoint: /var/lib/scylla
//	    ...
//
// Expected data shape:
//
//	{"files": {"cpuset.conf": {"CPUSET": "--cpuset 0,1,2,3"}, "io_properties.yaml": {...}}}
func parseScyllaDContents(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty scylla.d output"), nil
	}

	files := make(map[string]interface{})

	// Split on the "=== /path/to/file ===" markers.
	headerRegex := regexp.MustCompile(`(?m)^=== (.+) ===$`)
	locs := headerRegex.FindAllStringSubmatchIndex(content, -1)

	for i, loc := range locs {
		filePath := content[loc[2]:loc[3]]
		fileName := filepath.Base(filePath)

		// Extract file content between this header and the next (or end of string).
		contentStart := loc[1]
		var contentEnd int
		if i+1 < len(locs) {
			contentEnd = locs[i+1][0]
		} else {
			contentEnd = len(content)
		}
		fileContent := strings.TrimSpace(content[contentStart:contentEnd])

		ext := filepath.Ext(fileName)
		parsed := parseConfigFile(fileContent, ext)
		files[fileName] = parsed
	}

	data := map[string]interface{}{
		"files": files,
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// parseSchemaVersions parses the JSON output of the Scylla REST API
// /storage_proxy/schema_versions endpoint and produces data matching
// Scylla Doctor's ScyllaClusterSchemaCollector.
//
// Example input:
//
//	[{"key": "fae808bd-...", "value": ["10.0.0.1", "10.0.0.2"]}]
//
// The data shape is the raw JSON array — data is a list, not a dict.
func parseSchemaVersions(content string) (CollectorResult, error) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return NewSkippedResult("empty schema versions output"), nil
	}

	var data []interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return CollectorResult{}, fmt.Errorf("can't parse schema versions JSON: %w", err)
	}

	// Sort the value arrays within each entry, matching Scylla Doctor behavior.
	for _, entry := range data {
		if m, ok := entry.(map[string]interface{}); ok {
			if vals, ok := m["value"].([]interface{}); ok {
				sortStringSlice(vals)
			}
		}
	}

	return NewPassedResult(data, "Converted from must-gather"), nil
}

// Helper: parse a key=value file (like /etc/os-release).
func parseKeyValueFile(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := line[:idx]
		value := line[idx+1:]
		result[key] = value
	}
	return result
}

// Helper: strip surrounding quotes from a string.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// Helper: parse a config file content based on its extension.
// Returns a map for INI-style files, the parsed YAML structure, or raw string on failure.
func parseConfigFile(content string, ext string) interface{} {
	switch strings.ToLower(ext) {
	case ".yaml", ".yml":
		var result interface{}
		if err := yaml.Unmarshal([]byte(content), &result); err != nil {
			return content
		}
		return normalizeYAMLValue(result)

	case ".json":
		var result interface{}
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			return content
		}
		return result

	default:
		// INI-style key=value parsing for .conf, .cfg, etc.
		return parseINIFile(content)
	}
}

// Helper: parse INI-style config (simple KEY=VALUE or KEY="VALUE" format).
func parseINIFile(content string) map[string]interface{} {
	result := make(map[string]interface{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := stripQuotes(strings.TrimSpace(line[idx+1:]))
		result[key] = value
	}
	return result
}

// Helper: normalize YAML values from yaml.v2 (which uses map[interface{}]interface{})
// into JSON-compatible types (map[string]interface{}).
func normalizeYAMLValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[fmt.Sprintf("%v", k)] = normalizeYAMLValue(v)
		}
		return result
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeYAMLValue(item)
		}
		return val
	default:
		return v
	}
}

// Helper: sort a slice of interface{} values as strings (in-place).
func sortStringSlice(vals []interface{}) {
	strs := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			strs = append(strs, s)
		}
	}
	// Simple insertion sort (small slices).
	for i := 1; i < len(strs); i++ {
		for j := i; j > 0 && strs[j] < strs[j-1]; j-- {
			strs[j], strs[j-1] = strs[j-1], strs[j]
		}
	}
	for i, s := range strs {
		if i < len(vals) {
			vals[i] = s
		}
	}
}
