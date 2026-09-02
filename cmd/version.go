package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/davidpopovici01/grades/cmd.Version=vX.Y.Z".
var Version = "dev"

func newVersionCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Args:  cobra.NoArgs,
		Short: "Show the CLI version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(out, "grades %s\n", Version)
			return nil
		},
	}
}
