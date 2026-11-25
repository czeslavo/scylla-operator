// Package collectors gathers SODA's per-node vitals: Scylla Doctor's own (run inside the scylla container over
// exec) and SODA's Kubernetes-side collectors, which use the same vitals format.
package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/soda/contract"
	"github.com/scylladb/scylla-operator/pkg/soda/kube"
	"github.com/scylladb/scylla-operator/pkg/soda/vitals"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// The checks every other Scylla Doctor check depends on. If one of them did not pass, the node's vitals are
// incomplete for reasons that are not findings about the node (no scylla.yaml, CQL login, ...).
var keyScyllaDoctorCollectors = []string{
	"ScyllaConfigurationFileCollector",
	"CqlshCollector",
	"SystemConfigCollector",
}

type NodeCollectOptions struct {
	RunID string
	Pyz   []byte
	// CQLConfig is a Scylla Doctor configuration file with a [CQL] section, or nil when CQL needs no login.
	// It is written into the container over stdin, never passed as an argument.
	CQLConfig []byte
	Contract  *contract.Contract
	// Timeout bounds Scylla Doctor's run inside the container.
	Timeout time.Duration
}

type NodeOutput struct {
	Name            string
	Pod             *corev1.Pod
	ContractResults []contract.Result
	ContractError   string
	RawVitals       []byte
	Vitals          vitals.Vitals
	Stdout          []byte
	Stderr          []byte
	Collection      vitals.NodeCollection
}

// CollectScyllaDoctor runs the runtime contract check and Scylla Doctor in a node's scylla container.
// It never returns an error: failures are recorded in the output, so one node can't stop the run.
func CollectScyllaDoctor(ctx context.Context, execer *kube.Execer, pod *corev1.Pod, o NodeCollectOptions) *NodeOutput {
	start := time.Now()
	out := &NodeOutput{Name: pod.Name, Pod: pod}
	ns, name, ctr := pod.Namespace, pod.Name, naming.ScyllaContainerName

	run := func(timeout time.Duration, argv []string, stdin []byte) (*kube.ExecResult, error) {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if stdin == nil {
			return execer.Exec(cctx, ns, name, ctr, argv, nil)
		}
		return execer.Exec(cctx, ns, name, ctr, argv, bytes.NewReader(stdin))
	}
	fail := func(state, reason string) *NodeOutput {
		out.Collection.State = state
		out.Collection.Reason = reason
		out.Collection.DurationSeconds = time.Since(start).Seconds()
		klog.InfoS("Node collection finished", "Pod", ns+"/"+name, "State", state, "Reason", reason)
		return out
	}

	// 1. Runtime contract.
	res, err := run(time.Minute, []string{"sh", "-s"}, []byte(o.Contract.Script()))
	switch {
	case err != nil:
		out.ContractError = err.Error()
	default:
		out.ContractResults = o.Contract.ParseOutput(string(res.Stdout))
	}
	// Collection runs even when the contract isn't met: Scylla Doctor reports what it can, and SODA's runtime
	// contract analyzer reports the gap.
	for _, r := range out.ContractResults {
		if r.Level == contract.LevelRequired && !r.Satisfied {
			klog.InfoS("Runtime contract requirement not met", "Pod", ns+"/"+name, "Requirement", r.ID, "Detail", r.Detail)
		}
	}

	// 2. Stage Scylla Doctor and, only when needed, its CQL login. umask keeps sd.conf readable by root only.
	dir := "/tmp/soda-" + o.RunID
	defer func() {
		// A fresh context: a cancelled or timed-out run must still remove sd.conf.
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := execer.Exec(cctx, ns, name, ctr, []string{"rm", "-rf", "--", dir}, nil)
		if err != nil || res.ExitCode != 0 {
			klog.ErrorS(err, "Can't clean up the run directory", "Pod", ns+"/"+name, "Dir", dir)
		}
	}()
	res, err = run(time.Minute, []string{"sh", "-c", `umask 077 && mkdir -p "$0" && cat > "$0/scylla_doctor.pyz"`, dir}, o.Pyz)
	if err != nil || res.ExitCode != 0 {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("can't copy Scylla Doctor into the container: %v %s", err, trim(res)))
	}
	cfArgs := ""
	if o.CQLConfig != nil {
		res, err = run(time.Minute, []string{"sh", "-c", `umask 077 && cat > "$0/sd.conf"`, dir}, o.CQLConfig)
		if err != nil || res.ExitCode != 0 {
			return fail(vitals.NodeCollectionFailed, fmt.Sprintf("can't write the CQL login into the container: %v %s", err, trim(res)))
		}
		cfArgs = ` -cf sd.conf`
	}

	// 3. Run it. Output goes to files and is read back separately, so a hanging exec stream loses nothing.
	secs := int(o.Timeout.Seconds())
	script := fmt.Sprintf(`cd "$0" && timeout -k 10 %d %s scylla_doctor.pyz%s --save-vitals vitals.json > stdout.log 2> stderr.log; echo $? > exit`,
		secs, o.Contract.Interpreter, cfArgs)
	res, err = run(o.Timeout+time.Minute, []string{"sh", "-c", script, dir}, nil)
	if err != nil {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("running Scylla Doctor failed: %v", err))
	}

	// 4. Read the results back.
	read := func(file string) []byte {
		res, err := run(2*time.Minute, []string{"cat", dir + "/" + file}, nil)
		if err != nil || res.ExitCode != 0 {
			return nil
		}
		return res.Stdout
	}
	out.Stdout = read("stdout.log")
	out.Stderr = read("stderr.log")
	if code, err := strconv.Atoi(strings.TrimSpace(string(read("exit")))); err == nil {
		out.Collection.ExitCode = &code
	}
	out.RawVitals = read("vitals.json")
	if len(out.RawVitals) == 0 {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("Scylla Doctor wrote no vitals (exit code %s): %s",
			exitCodeString(out.Collection.ExitCode), lastLine(out.Stderr)))
	}
	if err := json.Unmarshal(out.RawVitals, &out.Vitals); err != nil {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("Scylla Doctor's vitals don't parse: %v", err))
	}

	// 5. Judge by the checks everything depends on; exit codes can't tell. Each one depends on the previous, so the
	// first that didn't pass is the root cause (later messages list prerequisites in no stable order).
	for _, c := range keyScyllaDoctorCollectors {
		r, ok := out.Vitals[c]
		if !ok {
			return fail(vitals.NodeCollectionDegraded, c+" is missing")
		}
		if r.Status != vitals.CollectorPassed {
			return fail(vitals.NodeCollectionDegraded, fmt.Sprintf("%s %s: %s", c, r.Status, r.Message))
		}
	}
	return fail(vitals.NodeCollectionSucceeded, "")
}

func trim(res *kube.ExecResult) string {
	if res == nil {
		return ""
	}
	return string(bytes.TrimSpace(res.Stderr))
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

func exitCodeString(c *int) string {
	if c == nil {
		return "unknown"
	}
	return strconv.Itoa(*c)
}
