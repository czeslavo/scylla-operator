package collectors

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseCQLSHTable parses the plain-text table output produced by cqlsh when
// run without --output-format json (which is not available in all versions).
//
// The expected format is:
//
//	<empty line>
//	 col1 | col2 | col3
//	------+------+------
//	 val1 | val2 | val3
//	 val4 | val5 | val6
//	<empty line>
//	(N rows)
//
// It returns a slice of maps, one map per data row, keyed by column name.
// Column names are trimmed of whitespace. Cell values are trimmed of whitespace.
func parseCQLSHTable(output string) ([]map[string]string, error) {
	lines := strings.Split(output, "\n")

	var headerLine string
	var dataLines []string
	separatorSeen := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and the "(N rows)" footer.
		if trimmed == "" || strings.HasPrefix(trimmed, "(") {
			continue
		}

		// The separator line contains only dashes and plus signs.
		if isSeparatorLine(trimmed) {
			separatorSeen = true
			continue
		}

		if !separatorSeen {
			// First non-empty, non-separator line is the header.
			if headerLine == "" {
				headerLine = line
			}
		} else {
			dataLines = append(dataLines, line)
		}
	}

	if headerLine == "" {
		return nil, fmt.Errorf("cqlsh output contains no header line")
	}

	headers := splitCQLRow(headerLine)

	rows := make([]map[string]string, 0, len(dataLines))
	for _, dl := range dataLines {
		cells := splitCQLRow(dl)
		if len(cells) != len(headers) {
			return nil, fmt.Errorf("column count mismatch: header has %d columns but data row has %d: %q", len(headers), len(cells), dl)
		}
		row := make(map[string]string, len(headers))
		for i, h := range headers {
			row[h] = cells[i]
		}
		rows = append(rows, row)
	}

	return rows, nil
}

// isSeparatorLine returns true if the trimmed line consists only of dashes and
// plus signs, which is the separator between the cqlsh header and data rows.
func isSeparatorLine(trimmed string) bool {
	if len(trimmed) == 0 {
		return false
	}
	for _, ch := range trimmed {
		if ch != '-' && ch != '+' {
			return false
		}
	}
	return true
}

// splitCQLRow splits a pipe-delimited cqlsh row into trimmed cell values.
// Leading and trailing pipes (if any) are ignored.
func splitCQLRow(line string) []string {
	parts := strings.Split(line, "|")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" && len(parts) == 1 {
			// Completely empty line — skip.
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

// cqlshRowsToJSON converts the rows returned by parseCQLSHTable into a
// compact JSON byte slice (array of objects). This is stored as the artifact.
func cqlshRowsToJSON(rows []map[string]string) ([]byte, error) {
	return json.Marshal(rows)
}
