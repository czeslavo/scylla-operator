// Copyright (C) 2026 ScyllaDB

package main

import (
	"os"

	"github.com/scylladb/scylla-operator/pkg/cmd/scylladbctl"
	"github.com/scylladb/scylla-operator/pkg/genericclioptions"
)

func main() {
	streams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	cmd := scylladbctl.NewScyllaDBCtlCommand(streams)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
