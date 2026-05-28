package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(bi buildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "runq %s %s %s\n", bi.version, bi.commit, bi.date)
			return err
		},
	}
}
