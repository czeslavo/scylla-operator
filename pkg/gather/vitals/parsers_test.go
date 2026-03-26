package vitals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseUname(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name           string
		input          string
		expectedStatus int
		expectedArch   string
		expectedKernel string
		expectError    bool
	}{
		{
			name:           "standard uname output",
			input:          "Linux scylla-node-0 5.15.0-91-generic #101-Ubuntu SMP Tue Nov 14 13:30:08 UTC 2023 x86_64 x86_64 x86_64 GNU/Linux\n",
			expectedStatus: StatusPassed,
			expectedArch:   "x86_64",
			expectedKernel: "5.15.0-91-generic",
		},
		{
			name:           "aarch64 uname output",
			input:          "Linux scylla-node-0 5.10.0-1234 #1 SMP aarch64 aarch64 aarch64 GNU/Linux\n",
			expectedStatus: StatusPassed,
			expectedArch:   "aarch64",
			expectedKernel: "5.10.0-1234",
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
		{
			name:           "whitespace only returns skipped",
			input:          "   \n\t  \n",
			expectedStatus: StatusSkipped,
		},
		{
			name:        "too few fields returns error",
			input:       "Linux hostname",
			expectError: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseUname(tc.input)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				if data["architecture"] != tc.expectedArch {
					t.Errorf("expected architecture %q, got %q", tc.expectedArch, data["architecture"])
				}
				if data["kernel_version"] != tc.expectedKernel {
					t.Errorf("expected kernel_version %q, got %q", tc.expectedKernel, data["kernel_version"])
				}
			}
		})
	}
}

func TestParseOsRelease(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name                 string
		input                string
		expectedStatus       int
		expectedName         string
		expectedVersion      string
		expectedVersionMinor string
	}{
		{
			name: "ubuntu 22.04",
			input: `PRETTY_NAME="Ubuntu 22.04.3 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
`,
			expectedStatus:       StatusPassed,
			expectedName:         "ubuntu",
			expectedVersion:      "22",
			expectedVersionMinor: "22.04",
		},
		{
			name: "centos 7 without quotes on ID",
			input: `NAME="CentOS Linux"
VERSION="7 (Core)"
ID=centos
VERSION_ID="7"
`,
			expectedStatus:       StatusPassed,
			expectedName:         "centos",
			expectedVersion:      "7",
			expectedVersionMinor: "7",
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseOsRelease(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				if data["name"] != tc.expectedName {
					t.Errorf("expected name %q, got %q", tc.expectedName, data["name"])
				}
				if data["version"] != tc.expectedVersion {
					t.Errorf("expected version %q, got %q", tc.expectedVersion, data["version"])
				}
				if data["version_minor"] != tc.expectedVersionMinor {
					t.Errorf("expected version_minor %q, got %q", tc.expectedVersionMinor, data["version_minor"])
				}
			}
		})
	}
}

func TestParseLscpu(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name               string
		input              string
		expectedStatus     int
		expectedCores      int
		expectedFlagsCount int
		expectedHasSSE42   bool
	}{
		{
			name: "standard lscpu output",
			input: `Architecture:          x86_64
CPU op-mode(s):        32-bit, 64-bit
Byte Order:            Little Endian
CPU(s):                8
On-line CPU(s) list:   0-7
Thread(s) per core:    2
Core(s) per socket:    4
Socket(s):             1
NUMA node(s):          1
Vendor ID:             GenuineIntel
Flags:                 fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx rdtscp lm constant_tsc sse4_2
`,
			expectedStatus:     StatusPassed,
			expectedCores:      8,
			expectedFlagsCount: 29,
			expectedHasSSE42:   true,
		},
		{
			name: "single CPU no flags line",
			input: `Architecture:          aarch64
CPU(s):                1
`,
			expectedStatus:     StatusPassed,
			expectedCores:      1,
			expectedFlagsCount: 0,
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseLscpu(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				if cores, ok := data["logical_cores"].(int); !ok || cores != tc.expectedCores {
					t.Errorf("expected logical_cores %d, got %v", tc.expectedCores, data["logical_cores"])
				}
				flags := data["flags"].([]string)
				if len(flags) != tc.expectedFlagsCount {
					t.Errorf("expected %d flags, got %d", tc.expectedFlagsCount, len(flags))
				}
				if tc.expectedHasSSE42 {
					found := false
					for _, f := range flags {
						if f == "sse4_2" {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected sse4_2 flag to be present")
					}
				}
			}
		})
	}
}

func TestParseFree(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name           string
		input          string
		expectedStatus int
		expectedTotal  int
		expectError    bool
	}{
		{
			name: "standard free output",
			input: `              total        used        free      shared  buff/cache   available
Mem:       16384000     8192000     4096000      512000     4096000    12288000
Swap:       2097152      524288     1572864
`,
			expectedStatus: StatusPassed,
			expectedTotal:  16384000,
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
		{
			name:        "no Mem line returns error",
			input:       "some random output\n",
			expectError: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseFree(tc.input)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				if total, ok := data["total"].(int); !ok || total != tc.expectedTotal {
					t.Errorf("expected total %d, got %v", tc.expectedTotal, data["total"])
				}
			}
		})
	}
}

func TestParseScyllaVersion(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name            string
		input           string
		expectedStatus  int
		expectedVersion string
		expectedEdition string
	}{
		{
			name:            "oss version",
			input:           "5.4.0-0.20231113.b4f3f037c635\n",
			expectedStatus:  StatusPassed,
			expectedVersion: "5.4.0-0.20231113.b4f3f037c635",
			expectedEdition: "oss",
		},
		{
			name:            "enterprise version",
			input:           "2023.1.8-enterprise-0.20231113.b4f3f037c635\n",
			expectedStatus:  StatusPassed,
			expectedVersion: "2023.1.8-enterprise-0.20231113.b4f3f037c635",
			expectedEdition: "enterprise",
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseScyllaVersion(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				if data["version"] != tc.expectedVersion {
					t.Errorf("expected version %q, got %q", tc.expectedVersion, data["version"])
				}
				if data["edition"] != tc.expectedEdition {
					t.Errorf("expected edition %q, got %q", tc.expectedEdition, data["edition"])
				}
			}
		})
	}
}

func TestParseScyllaDContents(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name           string
		input          string
		expectedStatus int
		expectedFiles  map[string]bool // Just check the filenames present.
	}{
		{
			name: "multiple config files",
			input: `=== /etc/scylla.d/cpuset.conf ===
CPUSET="--cpuset 0,1,2,3"
SCYLLA_ARGS="--smp 4"
=== /etc/scylla.d/io_properties.yaml ===
disks:
  - mountpoint: /var/lib/scylla
    read_iops: 100000
    read_bandwidth: 1000000000
    write_iops: 50000
    write_bandwidth: 500000000
=== /etc/scylla.d/dev-mode.conf ===
DEV_MODE="--developer-mode 1"
`,
			expectedStatus: StatusPassed,
			expectedFiles:  map[string]bool{"cpuset.conf": true, "io_properties.yaml": true, "dev-mode.conf": true},
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseScyllaDContents(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.(map[string]interface{})
				files := data["files"].(map[string]interface{})
				for name := range tc.expectedFiles {
					if _, ok := files[name]; !ok {
						t.Errorf("expected file %q to be present", name)
					}
				}
				if len(files) != len(tc.expectedFiles) {
					t.Errorf("expected %d files, got %d", len(tc.expectedFiles), len(files))
				}

				// Verify INI parsing of cpuset.conf.
				if cpuset, ok := files["cpuset.conf"].(map[string]interface{}); ok {
					if cpuset["CPUSET"] != "--cpuset 0,1,2,3" {
						t.Errorf("expected CPUSET value %q, got %q", "--cpuset 0,1,2,3", cpuset["CPUSET"])
					}
				}

				// Verify YAML parsing of io_properties.yaml.
				if ioProp, ok := files["io_properties.yaml"].(map[string]interface{}); ok {
					if _, ok := ioProp["disks"]; !ok {
						t.Errorf("expected 'disks' key in io_properties.yaml")
					}
				}
			}
		})
	}
}

func TestParseSchemaVersions(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name           string
		input          string
		expectedStatus int
		expectedLen    int
		expectError    bool
	}{
		{
			name:           "valid schema versions",
			input:          `[{"key":"fae808bd-d947-31cf-9516-f0e12a8410ab","value":["10.0.0.3","10.0.0.1","10.0.0.2"]}]`,
			expectedStatus: StatusPassed,
			expectedLen:    1,
		},
		{
			name:           "empty array",
			input:          `[]`,
			expectedStatus: StatusPassed,
			expectedLen:    0,
		},
		{
			name:           "empty input returns skipped",
			input:          "",
			expectedStatus: StatusSkipped,
		},
		{
			name:        "invalid JSON returns error",
			input:       `{not json`,
			expectError: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseSchemaVersions(tc.input)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, result.Status)
			}
			if tc.expectedStatus == StatusPassed {
				data := result.Data.([]interface{})
				if len(data) != tc.expectedLen {
					t.Errorf("expected %d entries, got %d", tc.expectedLen, len(data))
				}
				// Verify sorting of values.
				if tc.expectedLen > 0 {
					entry := data[0].(map[string]interface{})
					vals := entry["value"].([]interface{})
					if len(vals) == 3 {
						// Should be sorted: 10.0.0.1, 10.0.0.2, 10.0.0.3
						if vals[0] != "10.0.0.1" || vals[1] != "10.0.0.2" || vals[2] != "10.0.0.3" {
							t.Errorf("expected sorted values, got %v", vals)
						}
					}
				}
			}
		})
	}
}

func TestConverterConvert(t *testing.T) {
	t.Parallel()

	// Create a temporary must-gather directory structure.
	tmpDir := t.TempDir()
	podDir := filepath.Join(tmpDir, "namespaces", "scylla", "pods", "scylla-dc-rack-0")
	if err := os.MkdirAll(podDir, 0770); err != nil {
		t.Fatalf("can't create pod dir: %v", err)
	}

	// Write sample artifacts.
	artifacts := map[string]string{
		"uname.log":          "Linux scylla-dc-rack-0 5.15.0-91-generic #101-Ubuntu SMP x86_64 x86_64 x86_64 GNU/Linux\n",
		"os-release.log":     "ID=ubuntu\nVERSION_ID=\"22.04\"\n",
		"lscpu.log":          "Architecture:          x86_64\nCPU(s):                4\nFlags:                 sse sse2 sse4_2 avx\n",
		"free.log":           "              total        used        free\nMem:       16384000     8192000     4096000\n",
		"scylla-version.log": "5.4.0-0.20231113.b4f3f037c635\n",
	}

	for name, content := range artifacts {
		if err := os.WriteFile(filepath.Join(podDir, name), []byte(content), 0666); err != nil {
			t.Fatalf("can't write artifact %q: %v", name, err)
		}
	}

	outputDir := filepath.Join(tmpDir, "output")

	converter := NewConverter(tmpDir, outputDir)
	results, err := converter.Convert()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.PodName != "scylla-dc-rack-0" {
		t.Errorf("expected pod name %q, got %q", "scylla-dc-rack-0", result.PodName)
	}

	// Verify the vitals.json was written.
	vitalsData, err := os.ReadFile(result.OutputPath)
	if err != nil {
		t.Fatalf("can't read vitals file: %v", err)
	}

	var vitals map[string]CollectorResult
	if err := json.Unmarshal(vitalsData, &vitals); err != nil {
		t.Fatalf("can't unmarshal vitals JSON: %v", err)
	}

	// We should have 5 collectors (matching the 5 artifacts we wrote).
	expectedCollectors := []string{
		"ComputerArchitectureCollector",
		"OSCollector",
		"CPUSpecificationsCollector",
		"RAMCollector",
		"ScyllaVersionCollector",
	}
	for _, name := range expectedCollectors {
		cr, ok := vitals[name]
		if !ok {
			t.Errorf("expected collector %q in vitals", name)
			continue
		}
		if cr.Status != StatusPassed {
			t.Errorf("expected collector %q to have PASSED status, got %d", name, cr.Status)
		}
	}
}

func TestConverterConvertWithSchemaVersions(t *testing.T) {
	t.Parallel()

	// Verify synthetic SystemConfigCollector is added when schema versions are present.
	tmpDir := t.TempDir()
	podDir := filepath.Join(tmpDir, "namespaces", "scylla", "pods", "scylla-dc-rack-0")
	if err := os.MkdirAll(podDir, 0770); err != nil {
		t.Fatalf("can't create pod dir: %v", err)
	}

	artifacts := map[string]string{
		"scylla-api-schema-versions.log": `[{"key":"fae808bd-d947-31cf-9516-f0e12a8410ab","value":["10.0.0.1"]}]`,
	}

	for name, content := range artifacts {
		if err := os.WriteFile(filepath.Join(podDir, name), []byte(content), 0666); err != nil {
			t.Fatalf("can't write artifact %q: %v", name, err)
		}
	}

	outputDir := filepath.Join(tmpDir, "output")

	converter := NewConverter(tmpDir, outputDir)
	results, err := converter.Convert()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	vitalsData, err := os.ReadFile(results[0].OutputPath)
	if err != nil {
		t.Fatalf("can't read vitals file: %v", err)
	}

	var vitals map[string]json.RawMessage
	if err := json.Unmarshal(vitalsData, &vitals); err != nil {
		t.Fatalf("can't unmarshal vitals JSON: %v", err)
	}

	if _, ok := vitals["SystemConfigCollector"]; !ok {
		t.Errorf("expected synthetic SystemConfigCollector entry")
	}
	if _, ok := vitals["ScyllaClusterSchemaCollector"]; !ok {
		t.Errorf("expected ScyllaClusterSchemaCollector entry")
	}
}

func TestVitalsJSONFormat(t *testing.T) {
	t.Parallel()

	// Verify the JSON output matches Scylla Doctor's expected format.
	result := NewPassedResult(map[string]interface{}{
		"version": "5.4.0",
	}, "test message")

	jsonData, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("can't marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("can't unmarshal: %v", err)
	}

	// Scylla Doctor requires these keys.
	requiredKeys := []string{"status", "data", "output", "message", "mask"}
	for _, key := range requiredKeys {
		if _, ok := parsed[key]; !ok {
			t.Errorf("required key %q missing from JSON", key)
		}
	}

	// Status should be numeric 0.
	if status, ok := parsed["status"].(float64); !ok || status != 0 {
		t.Errorf("expected status 0, got %v", parsed["status"])
	}

	// Output should be an empty array, not null.
	if output, ok := parsed["output"].([]interface{}); !ok {
		t.Errorf("expected output to be an array, got %T", parsed["output"])
	} else if len(output) != 0 {
		t.Errorf("expected empty output array, got %d items", len(output))
	}

	// Mask should be an empty array, not null.
	if mask, ok := parsed["mask"].([]interface{}); !ok {
		t.Errorf("expected mask to be an array, got %T", parsed["mask"])
	} else if len(mask) != 0 {
		t.Errorf("expected empty mask array, got %d items", len(mask))
	}
}

func TestParseINIFile(t *testing.T) {
	t.Parallel()

	input := `# Comment line
CPUSET="--cpuset 0,1,2,3"
SCYLLA_ARGS="--smp 4"
; Another comment
EMPTY_KEY=
`
	result := parseINIFile(input)
	expected := map[string]interface{}{
		"CPUSET":      "--cpuset 0,1,2,3",
		"SCYLLA_ARGS": "--smp 4",
		"EMPTY_KEY":   "",
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestNormalizeYAMLValue(t *testing.T) {
	t.Parallel()

	input := map[interface{}]interface{}{
		"key1": "value1",
		"key2": []interface{}{
			map[interface{}]interface{}{
				"nested": 42,
			},
		},
	}

	result := normalizeYAMLValue(input)
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if m["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", m["key1"])
	}
	arr, ok := m["key2"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("expected key2 to be array of length 1")
	}
	nested, ok := arr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map[string]interface{}, got %T", arr[0])
	}
	if nested["nested"] != 42 {
		t.Errorf("expected nested=42, got %v", nested["nested"])
	}
}
