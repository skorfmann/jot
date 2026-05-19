package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

type Root struct {
	out       io.Writer
	serverURL string
	jsonOut   bool
}

func Execute() error {
	root := NewRoot(os.Stdout)
	return root.Execute()
}

func NewRoot(out io.Writer) *cobra.Command {
	r := &Root{out: out}
	cmd := &cobra.Command{
		Use:           "jot",
		Short:         "Push private static artifacts to a self-hosted jot server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&r.serverURL, "server", "", "Jot server URL. Overrides $JOT_SERVER and ~/.config/jot/config.toml.")
	cmd.AddCommand(r.loginCmd())
	cmd.AddCommand(r.logoutCmd())
	cmd.AddCommand(r.pushCmd())
	cmd.AddCommand(r.listCmd())
	cmd.AddCommand(r.inspectCmd())
	cmd.AddCommand(r.historyCmd())
	cmd.AddCommand(r.rollbackCmd())
	cmd.AddCommand(r.rmCmd())
	cmd.AddCommand(r.whoamiCmd())
	cmd.AddCommand(r.initCmd())
	return cmd
}

func (r *Root) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.out, format, args...)
}
