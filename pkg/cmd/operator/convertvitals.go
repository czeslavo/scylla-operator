package operator

import (
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/gather/vitals"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	convertVitalsLongDescription = templates.LongDesc(`
		convert-vitals converts a must-gather artifact directory into Scylla Doctor
		vitals JSON files that can be loaded with 'scylla-doctor --load-vitals'.

		This command reads the diagnostic artifacts collected by 'must-gather' from
		Scylla pods (uname, os-release, lscpu, free, scylla --version, scylla.d
		config files, and REST API output), and produces a vitals.json per pod that
		matches Scylla Doctor's expected schema.

		This command is experimental and subject to change without notice.
	`)

	convertVitalsExample = templates.Examples(`
		# Convert must-gather output into Scylla Doctor vitals files.
		scylla-operator convert-vitals --must-gather-dir=./must-gather-output --output-dir=./vitals-output

		# Convert and write vitals files alongside the must-gather artifacts.
		scylla-operator convert-vitals --must-gather-dir=./must-gather-output
	`)
)

type ConvertVitalsOptions struct {
	MustGatherDir string
	OutputDir     string
}

func NewConvertVitalsOptions() *ConvertVitalsOptions {
	return &ConvertVitalsOptions{
		MustGatherDir: "",
		OutputDir:     "",
	}
}

func (o *ConvertVitalsOptions) AddFlags(flagset *pflag.FlagSet) {
	flagset.StringVar(&o.MustGatherDir, "must-gather-dir", o.MustGatherDir, "Path to the must-gather output directory containing collected artifacts.")
	flagset.StringVar(&o.OutputDir, "output-dir", o.OutputDir, "Path to write vitals JSON files. If empty, vitals are written alongside the must-gather artifacts.")
}

func (o *ConvertVitalsOptions) Validate() error {
	var errs []error

	if len(o.MustGatherDir) == 0 {
		errs = append(errs, fmt.Errorf("--must-gather-dir is required"))
	}

	return apimachineryutilerrors.NewAggregate(errs)
}

func (o *ConvertVitalsOptions) Complete() error {
	if len(o.OutputDir) == 0 {
		o.OutputDir = o.MustGatherDir
	}
	return nil
}

func (o *ConvertVitalsOptions) Run() error {
	klog.InfoS("Converting must-gather artifacts to Scylla Doctor vitals", "MustGatherDir", o.MustGatherDir, "OutputDir", o.OutputDir)

	converter := vitals.NewConverter(o.MustGatherDir, o.OutputDir)
	results, err := converter.Convert()
	if err != nil {
		return fmt.Errorf("can't convert must-gather artifacts: %w", err)
	}

	klog.InfoS("Conversion complete", "VitalsFilesWritten", len(results))
	for _, r := range results {
		klog.InfoS("Wrote vitals file", "Pod", r.PodName, "Path", r.OutputPath, "Collectors", len(r.CollectorCount))
	}

	return nil
}

func NewConvertVitalsCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewConvertVitalsOptions()

	cmd := &cobra.Command{
		Use:     "convert-vitals",
		Short:   "Convert must-gather artifacts to Scylla Doctor vitals format.",
		Long:    convertVitalsLongDescription,
		Example: convertVitalsExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := o.Validate()
			if err != nil {
				return err
			}

			err = o.Complete()
			if err != nil {
				return err
			}

			err = o.Run()
			if err != nil {
				return err
			}

			return nil
		},
		ValidArgs: []string{},

		SilenceErrors: true,
		SilenceUsage:  true,
	}

	o.AddFlags(cmd.Flags())

	return cmd
}
