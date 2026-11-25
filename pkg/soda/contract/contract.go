// Package contract describes what Scylla Doctor needs from the ScyllaDB image it runs in, and checks it.
//
// The contract ships in the Scylla Doctor bundle (runtime-contract.json), so the bundle declares what its
// Scylla Doctor version needs. SODA turns it into a shell script, runs it in each scylla container before
// collection, and records the result as a collector in SODA's vitals.
package contract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	LevelRequired    = "required"
	LevelRecommended = "recommended"
	LevelOptional    = "optional"
)

type Check struct {
	Type   string   `json:"type"`
	Path   string   `json:"path,omitempty"`
	Names  []string `json:"names,omitempty"`
	Module string   `json:"module,omitempty"`
	Min    string   `json:"min,omitempty"`
}

type Requirement struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Check  Check  `json:"check"`
	UsedBy string `json:"usedBy"`
	Impact string `json:"impact"`
}

type Service struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
	UsedBy   string `json:"usedBy"`
}

type Contract struct {
	SchemaVersion       string        `json:"schemaVersion"`
	ScyllaDoctorVersion string        `json:"scyllaDoctorVersion"`
	Runtime             string        `json:"runtime"`
	Interpreter         string        `json:"interpreter"`
	Notes               []string      `json:"notes"`
	Requirements        []Requirement `json:"requirements"`
	Services            []Service     `json:"services"`
}

// Result is the outcome of one requirement's check in one container.
type Result struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	Satisfied bool   `json:"satisfied"`
	Detail    string `json:"detail,omitempty"`
	UsedBy    string `json:"usedBy"`
	Impact    string `json:"impact"`
}

func Parse(b []byte) (*Contract, error) {
	c := &Contract{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("can't parse runtime contract: %w", err)
	}
	if len(c.Interpreter) == 0 {
		return nil, fmt.Errorf("runtime contract has no interpreter")
	}
	for _, r := range c.Requirements {
		switch r.Level {
		case LevelRequired, LevelRecommended, LevelOptional:
		default:
			return nil, fmt.Errorf("requirement %q has unknown level %q", r.ID, r.Level)
		}
		switch r.Check.Type {
		case "executable", "file", "directory", "writableDirectory", "command", "pythonModule", "pythonVersion":
		default:
			return nil, fmt.Errorf("requirement %q has unknown check type %q", r.ID, r.Check.Type)
		}
	}
	return c, nil
}

const linePrefix = "SODA-CHECK"

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Script returns a POSIX shell script that checks every requirement and prints one line per requirement:
// "SODA-CHECK <id> <ok|missing> <detail>", tab-separated. It only reads; it is fed to `sh -s` over exec.
func (c *Contract) Script() string {
	var b strings.Builder
	b.WriteString("r() { printf '%s\\t%s\\t%s\\t%s\\n' " + linePrefix + " \"$1\" \"$2\" \"$3\"; }\n")
	py := shQuote(c.Interpreter)
	for _, req := range c.Requirements {
		id := shQuote(req.ID)
		ch := req.Check
		switch ch.Type {
		case "executable":
			fmt.Fprintf(&b, "if [ -x %[1]s ]; then r %[2]s ok %[1]s; else r %[2]s missing %[1]s; fi\n", shQuote(ch.Path), id)
		case "file":
			fmt.Fprintf(&b, "if [ -f %[1]s ]; then r %[2]s ok %[1]s; else r %[2]s missing %[1]s; fi\n", shQuote(ch.Path), id)
		case "directory":
			fmt.Fprintf(&b, "if [ -d %[1]s ]; then r %[2]s ok %[1]s; else r %[2]s missing %[1]s; fi\n", shQuote(ch.Path), id)
		case "writableDirectory":
			fmt.Fprintf(&b, "if [ -d %[1]s ] && [ -w %[1]s ]; then r %[2]s ok %[1]s; else r %[2]s missing \"not writable: \"%[1]s; fi\n", shQuote(ch.Path), id)
		case "command":
			names := make([]string, 0, len(ch.Names))
			for _, n := range ch.Names {
				names = append(names, shQuote(n))
			}
			fmt.Fprintf(&b, "m=''; for c in %s; do command -v \"$c\" >/dev/null 2>&1 || m=\"$m $c\"; done; "+
				"if [ -z \"$m\" ]; then r %s ok ''; else r %s missing \"not found:$m\"; fi\n", strings.Join(names, " "), id, id)
		case "pythonModule":
			fmt.Fprintf(&b, "if %s -c %s >/dev/null 2>&1; then r %s ok %s; else r %s missing %s; fi\n",
				py, shQuote("import "+ch.Module), id, shQuote(ch.Module), id, shQuote(ch.Module))
		case "pythonVersion":
			fmt.Fprintf(&b, "v=$(%s -c %s 2>/dev/null) && r %s ok \"$v\" || r %s missing 'interpreter did not run'\n",
				py, shQuote(`import sys; print("%d.%d.%d" % sys.version_info[:3])`), id, id)
		}
	}
	return b.String()
}

// ParseOutput turns the script's output into results, in contract order. Requirements with no output line are
// reported as not satisfied.
func (c *Contract) ParseOutput(out string) []Result {
	seen := map[string][2]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 4)
		if len(parts) < 3 || parts[0] != linePrefix {
			continue
		}
		detail := ""
		if len(parts) == 4 {
			detail = parts[3]
		}
		seen[parts[1]] = [2]string{parts[2], detail}
	}

	results := make([]Result, 0, len(c.Requirements))
	for _, req := range c.Requirements {
		res := Result{ID: req.ID, Level: req.Level, UsedBy: req.UsedBy, Impact: req.Impact}
		got, ok := seen[req.ID]
		switch {
		case !ok:
			res.Detail = "no result from the check script"
		default:
			res.Satisfied = got[0] == "ok"
			res.Detail = got[1]
			if res.Satisfied && req.Check.Type == "pythonVersion" && len(req.Check.Min) > 0 && !versionAtLeast(got[1], req.Check.Min) {
				res.Satisfied = false
				res.Detail = fmt.Sprintf("%s is older than %s", got[1], req.Check.Min)
			}
		}
		results = append(results, res)
	}
	return results
}

func versionAtLeast(have, min string) bool {
	h := strings.Split(have, ".")
	m := strings.Split(min, ".")
	for i := 0; i < len(m); i++ {
		mi, _ := strconv.Atoi(m[i])
		hi := 0
		if i < len(h) {
			hi, _ = strconv.Atoi(h[i])
		}
		if hi != mi {
			return hi > mi
		}
	}
	return true
}
