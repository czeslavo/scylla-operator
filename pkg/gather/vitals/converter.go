package vitals

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

// Convert walks the must-gather directory, finds pod artifact directories,
// parses collected files, and writes vitals.json per pod.
func (c *Converter) Convert() ([]ConversionResult, error) {
	// TODO: implement in Step 3
	return nil, nil
}
