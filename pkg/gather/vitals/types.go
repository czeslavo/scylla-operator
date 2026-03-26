package vitals

// CollectorResult represents a single collector's output in the Scylla Doctor
// vitals JSON format. The JSON representation matches what Scylla Doctor expects
// when loading vitals via --load-vitals.
type CollectorResult struct {
	Status  int           `json:"status"`
	Data    interface{}   `json:"data"`
	Output  []OutputEntry `json:"output"`
	Message string        `json:"message"`
	Mask    []interface{} `json:"mask"`
}

// OutputEntry represents a single output entry within a CollectorResult.
type OutputEntry struct {
	Level int    `json:"level"`
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Collector status constants matching Scylla Doctor's CollectorStatus enum.
const (
	StatusPassed  = 0
	StatusFailed  = 1
	StatusSkipped = 2
)

// Output entry level constants matching Scylla Doctor's Level IntEnum.
const (
	LevelDefault  = 0
	LevelDetailed = 1
	LevelVerbose  = 2
)

// Output entry type constants matching Scylla Doctor's OutputEntryType.
const (
	OutputTypeCommandOutput = "Command output"
	OutputTypeFileContent   = "File content"
	OutputTypeParsedContent = "Parsed content of"
	OutputTypeAPIOutput     = "API output from"
	OutputTypeCQLOutput     = "CQL output of"
	OutputTypeValue         = "Value of"
)

// NewPassedResult creates a CollectorResult with PASSED status.
func NewPassedResult(data interface{}, message string) CollectorResult {
	return CollectorResult{
		Status:  StatusPassed,
		Data:    data,
		Output:  []OutputEntry{},
		Message: message,
		Mask:    []interface{}{},
	}
}

// NewSkippedResult creates a CollectorResult with SKIPPED status.
func NewSkippedResult(message string) CollectorResult {
	return CollectorResult{
		Status:  StatusSkipped,
		Data:    map[string]interface{}{},
		Output:  []OutputEntry{},
		Message: message,
		Mask:    []interface{}{},
	}
}
