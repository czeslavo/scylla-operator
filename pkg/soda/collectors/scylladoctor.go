// Package collectors gathers SODA's per-node vitals: Scylla Doctor's own (run inside the scylla container over
// exec) and SODA's Kubernetes-side collectors, which use the same vitals format.
package collectors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	// SideFilesDir receives files Scylla Doctor wrote next to its vitals; SideFilesArchiveDir is the same directory
	// relative to the archive root. SideFilesLimitBytes caps their total size per node.
	SideFilesDir        string
	SideFilesArchiveDir string
	SideFilesLimitBytes int64
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
	SideFiles       []vitals.SideFile
	Collection      vitals.NodeCollection
}

// sodaRunFiles are the files SODA itself puts in or creates in the run directory; everything else there was
// written by Scylla Doctor.
var sodaRunFiles = map[string]bool{
	"scylla_doctor.pyz": true, "sd.conf": true, "vitals.json": true, "stdout.log": true, "stderr.log": true,
	"exit": true, "pid": true,
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
		// A fresh context: a cancelled or timed-out run must still stop Scylla Doctor and remove sd.conf. Closing an
		// exec stream doesn't stop the command, so a run that hasn't written its exit code is stopped by its PID
		// (timeout's, which passes the signal on to Scylla Doctor).
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanup := `if [ -f "$0/pid" ] && [ ! -f "$0/exit" ]; then kill -TERM "$(cat "$0/pid")" 2>/dev/null; fi; rm -rf -- "$0"`
		res, err := execer.Exec(cctx, ns, name, ctr, []string{"sh", "-c", cleanup, dir}, nil)
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

	// 3. Run it. Output goes to files and is read back separately, so an exec stream that hangs or breaks after
	// Scylla Doctor finished loses nothing: the results are still read back below.
	// TMPDIR points into the run directory: Scylla Doctor writes side files (the log collectors' journalctl output)
	// to the temporary directory, so they are picked up below and removed with the run directory.
	secs := int(o.Timeout.Seconds())
	script := fmt.Sprintf(`cd "$0" || exit 1; TMPDIR="$0" timeout -k 10 %d %s scylla_doctor.pyz%s --save-vitals vitals.json > stdout.log 2> stderr.log & `+
		`echo $! > pid; wait $!; echo $? > exit`, secs, o.Contract.Interpreter, cfArgs)
	_, runErr := run(o.Timeout+time.Minute, []string{"sh", "-c", script, dir}, nil)
	if runErr != nil {
		klog.InfoS("The exec running Scylla Doctor ended with an error; reading back what it wrote", "Pod", ns+"/"+name, "Error", runErr)
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
	if out.Collection.ExitCode == nil {
		// No exit code: Scylla Doctor didn't finish. The deferred cleanup stops it.
		reason := "Scylla Doctor didn't finish"
		if runErr != nil {
			reason += ": " + runErr.Error()
		}
		return fail(vitals.NodeCollectionFailed, reason)
	}
	out.RawVitals = read("vitals.json")
	if len(out.RawVitals) == 0 {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("Scylla Doctor wrote no vitals (exit code %s): %s",
			exitCodeString(out.Collection.ExitCode), lastLine(out.Stderr)))
	}
	if err := json.Unmarshal(out.RawVitals, &out.Vitals); err != nil {
		return fail(vitals.NodeCollectionFailed, fmt.Sprintf("Scylla Doctor's vitals don't parse: %v", err))
	}
	out.SideFiles = collectSideFiles(ctx, execer, pod, dir, o)

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

// collectSideFiles copies files Scylla Doctor left in the run directory into the archive, up to a size budget.
// Failures are recorded per file; they never fail the node.
func collectSideFiles(ctx context.Context, execer *kube.Execer, pod *corev1.Pod, dir string, o NodeCollectOptions) []vitals.SideFile {
	ns, name, ctr := pod.Namespace, pod.Name, naming.ScyllaContainerName
	list := `cd "$0" || exit 1; for f in * .[!.]*; do [ -f "$f" ] && printf '%s\t%s\n' "$(wc -c < "$f")" "$f"; done; true`
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	res, err := execer.Exec(cctx, ns, name, ctr, []string{"sh", "-c", list, dir}, nil)
	cancel()
	if err != nil || res.ExitCode != 0 {
		klog.ErrorS(err, "Can't list Scylla Doctor's side files", "Pod", ns+"/"+name)
		return nil
	}

	var files []vitals.SideFile
	budget := o.SideFilesLimitBytes
	for _, line := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		size, fname, ok := strings.Cut(line, "\t")
		if !ok || sodaRunFiles[fname] || strings.ContainsAny(fname, "/\\") {
			continue
		}
		sf := vitals.SideFile{Name: fname}
		sf.SizeBytes, _ = strconv.ParseInt(strings.TrimSpace(size), 10, 64)
		if sf.SizeBytes > budget {
			sf.Skipped = fmt.Sprintf("over the per-node side file limit (%d bytes left)", budget)
			files = append(files, sf)
			continue
		}
		if err := copySideFile(ctx, execer, pod, dir+"/"+fname, filepath.Join(o.SideFilesDir, fname), &sf); err != nil {
			sf.Skipped = err.Error()
		} else {
			budget -= sf.SizeBytes
			sf.ArchivePath = filepath.ToSlash(filepath.Join(o.SideFilesArchiveDir, fname))
		}
		files = append(files, sf)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}

func copySideFile(ctx context.Context, execer *kube.Execer, pod *corev1.Pod, src, dst string, sf *vitals.SideFile) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	code, stderr, err := execer.ExecToWriter(cctx, pod.Namespace, pod.Name, naming.ScyllaContainerName, []string{"cat", src}, io.MultiWriter(f, h))
	if err == nil && code != 0 {
		err = fmt.Errorf("cat exited with %d: %s", code, bytes.TrimSpace(stderr))
	}
	if err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("can't copy it: %w", err)
	}
	sf.SHA256 = hex.EncodeToString(h.Sum(nil))
	return f.Close()
}
