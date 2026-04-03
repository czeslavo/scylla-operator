package operator

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/scylladb/scylla-operator/pkg/signals"
	"github.com/scylladb/scylla-operator/pkg/soda/analyzers"
	"github.com/scylladb/scylla-operator/pkg/soda/artifacts"
	"github.com/scylladb/scylla-operator/pkg/soda/collectors"
	"github.com/scylladb/scylla-operator/pkg/soda/engine"
	"github.com/scylladb/scylla-operator/pkg/soda/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	serveReportLongDescription = templates.LongDesc(`
		serve-report starts a local HTTP server that serves an interactive HTML
		diagnostic report built from a previous 'diagnose' output directory.

		The report organizes data by ScyllaDB cluster, presenting the topology
		(Cluster → Datacenter → Rack → Node) alongside collector vitals, analysis
		results, and browsable artifact files.

		Point --output-dir at a directory produced by 'scylla-operator diagnose',
		or use --from-archive to load a .tar.gz archive.
	`)

	serveReportExample = templates.Examples(`
		# Serve a report from a diagnose output directory.
		scylla-operator diagnose serve-report --output-dir=/tmp/scylla-diagnose-abc123

		# Serve a report from a .tar.gz archive.
		scylla-operator diagnose serve-report --from-archive=scylla-diagnose-abc123.tar.gz

		# Serve from the current working directory (default).
		scylla-operator diagnose serve-report
	`)
)

// ServeReportOptions holds the options for the serve-report subcommand.
type ServeReportOptions struct {
	OutputDir   string // Path to an extracted diagnose output directory.
	FromArchive string // Path to a .tar.gz archive to extract first.

	// Resolved at Complete() time.
	dataDir string // Effective directory to read from.
	tempDir string // If non-empty, a temp directory that must be cleaned up.
}

// NewServeReportOptions creates ServeReportOptions with default values.
func NewServeReportOptions() *ServeReportOptions {
	return &ServeReportOptions{}
}

// AddFlags registers serve-report flags on the given flagset.
func (o *ServeReportOptions) AddFlags(flagset *pflag.FlagSet) {
	flagset.StringVar(&o.OutputDir, "output-dir", o.OutputDir, "Path to a diagnose output directory. Defaults to the current working directory.")
	flagset.StringVar(&o.FromArchive, "from-archive", o.FromArchive, "Path to a .tar.gz archive produced by 'diagnose --archive'. Extracts to a temp directory for serving.")
}

// NewServeReportCmd creates the serve-report cobra command.
func NewServeReportCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewServeReportOptions()

	cmd := &cobra.Command{
		Use:     "serve-report",
		Short:   "Start a local HTTP server to browse an interactive diagnostic report.",
		Long:    serveReportLongDescription,
		Example: serveReportExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Complete(); err != nil {
				return err
			}
			return o.Run(streams)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	o.AddFlags(cmd.Flags())
	return cmd
}

// Validate checks ServeReportOptions for invalid configurations.
func (o *ServeReportOptions) Validate() error {
	if o.OutputDir != "" && o.FromArchive != "" {
		return fmt.Errorf("--output-dir and --from-archive are mutually exclusive")
	}
	if o.FromArchive != "" {
		if _, err := os.Stat(o.FromArchive); err != nil {
			return fmt.Errorf("cannot access archive %q: %w", o.FromArchive, err)
		}
	}
	if o.OutputDir != "" {
		if _, err := os.Stat(o.OutputDir); err != nil {
			return fmt.Errorf("cannot access output directory %q: %w", o.OutputDir, err)
		}
	}
	return nil
}

// Complete resolves the effective data directory.
func (o *ServeReportOptions) Complete() error {
	if o.FromArchive != "" {
		tmpDir, err := os.MkdirTemp("", "scylla-serve-report-*")
		if err != nil {
			return fmt.Errorf("creating temp directory: %w", err)
		}
		o.tempDir = tmpDir

		if err := artifacts.ExtractTarGz(o.FromArchive, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			return fmt.Errorf("extracting archive %s: %w", o.FromArchive, err)
		}
		o.dataDir = tmpDir
		return nil
	}

	if o.OutputDir != "" {
		o.dataDir = o.OutputDir
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current working directory: %w", err)
		}
		o.dataDir = cwd
	}

	return nil
}

// Run loads diagnostic data and starts the HTTP server.
func (o *ServeReportOptions) Run(streams genericclioptions.IOStreams) error {
	// Clean up temp directory on exit if we created one.
	if o.tempDir != "" {
		defer os.RemoveAll(o.tempDir)
	}

	// Load vitals.json.
	vitalsPath := filepath.Join(o.dataDir, "vitals.json")
	vitalsData, err := os.ReadFile(vitalsPath)
	if err != nil {
		return fmt.Errorf("reading vitals.json from %s: %w (is this a diagnose output directory?)", vitalsPath, err)
	}

	var sv engine.SerializableVitals
	if err := json.Unmarshal(vitalsData, &sv); err != nil {
		return fmt.Errorf("parsing vitals.json: %w", err)
	}

	vitals, err := engine.FromSerializable(&sv, collectors.ResultTypeRegistry())
	if err != nil {
		return fmt.Errorf("deserializing vitals: %w", err)
	}

	// Reconstruct topology.
	clusterInfos, podsByCluster := engine.ClusterTopologyFromVitals(&sv, vitals)

	// Load report.json for analysis results (optional — may not exist in older archives).
	var jsonReport *output.JSONReport
	reportPath := filepath.Join(o.dataDir, "report.json")
	if reportData, err := os.ReadFile(reportPath); err == nil {
		var report output.JSONReport
		if err := json.Unmarshal(reportData, &report); err != nil {
			klog.V(2).InfoS("Failed to parse report.json, analysis results will be unavailable", "error", err)
		} else {
			jsonReport = &report
		}
	}

	// Build collector/analyzer name maps.
	allCollectorMap := collectors.AllCollectorsMap()
	allAnalyzerMap := analyzers.AllAnalyzersMap()
	collectorNames := make(map[engine.CollectorID]string, len(allCollectorMap))
	for id, c := range allCollectorMap {
		collectorNames[id] = c.Name()
	}
	analyzerNames := make(map[engine.AnalyzerID]string, len(allAnalyzerMap))
	for id, a := range allAnalyzerMap {
		analyzerNames[id] = a.Name()
	}

	// Build the HTML report data.
	reportData := output.BuildHTMLReportData(
		clusterInfos,
		podsByCluster,
		vitals,
		jsonReport,
		collectorNames,
		analyzerNames,
	)

	// Render the HTML.
	htmlContent, err := output.RenderHTML(reportData)
	if err != nil {
		return fmt.Errorf("rendering HTML report: %w", err)
	}

	// Set up HTTP server.
	mux := http.NewServeMux()

	// Serve the HTML report at /.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(htmlContent)
	})

	// Serve artifact files from the collectors/ subdirectory.
	collectorsDir := filepath.Join(o.dataDir, "collectors")
	mux.Handle("/artifacts/", http.StripPrefix("/artifacts/",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Sanitize the path to prevent directory traversal.
			cleanPath := filepath.Clean(r.URL.Path)
			if strings.Contains(cleanPath, "..") {
				http.NotFound(w, r)
				return
			}
			fullPath := filepath.Join(collectorsDir, cleanPath)

			// Ensure the resolved path is within the collectors directory.
			if !strings.HasPrefix(fullPath, collectorsDir) {
				http.NotFound(w, r)
				return
			}

			http.ServeFile(w, r, fullPath)
		}),
	))

	// Listen on a random available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("starting listener: %w", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	url := fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	fmt.Fprintf(streams.Out, "Serving interactive diagnostic report at: %s\n", url)
	fmt.Fprintf(streams.Out, "Press Ctrl+C to stop.\n")

	// Run server until signal.
	stopCh := signals.StopChannel()
	srv := &http.Server{Handler: mux}

	go func() {
		<-stopCh
		klog.InfoS("Shutting down report server")
		srv.Close()
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
