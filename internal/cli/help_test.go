package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpIncludesExamples(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root", args: []string{"--help"}, want: "jot push ./report.html --title"},
		{name: "login", args: []string{"login", "--help"}, want: "jot login --server https://jot.example.com"},
		{name: "logout", args: []string{"logout", "--help"}, want: "jot logout --server https://jot.example.com"},
		{name: "push", args: []string{"push", "--help"}, want: "jot push ./dist --as dashboard"},
		{name: "ls", args: []string{"ls", "--help"}, want: "jot ls --tag report --search"},
		{name: "inspect", args: []string{"inspect", "--help"}, want: "jot inspect 01HXABCDEFGHJKMNPQRSTVWXYZ"},
		{name: "history", args: []string{"history", "--help"}, want: "jot history dashboard --json"},
		{name: "rollback", args: []string{"rollback", "--help"}, want: "jot rollback dashboard 01HXABCDEFGHJKMNPQRSTVWXYZ"},
		{name: "rm", args: []string{"rm", "--help"}, want: "jot rm dashboard"},
		{name: "whoami", args: []string{"whoami", "--help"}, want: "jot whoami --server https://jot.example.com"},
		{name: "init", args: []string{"init", "--help"}, want: "jot init server > jot.yaml"},
		{name: "init server", args: []string{"init", "server", "--help"}, want: "jot-server --config jot.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := NewRoot(&buf)
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if !strings.Contains(out, "Examples:") {
				t.Fatalf("help output missing Examples section:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("help output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

func TestPushHelpSummaryFlagIsNotMangled(t *testing.T) {
	var buf bytes.Buffer
	cmd := NewRoot(&buf)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"push", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "--summary jot ls --search") {
		t.Fatalf("summary flag metavar was mangled by pflag backtick parsing:\n%s", out)
	}
	if !strings.Contains(out, "--summary string") {
		t.Fatalf("summary flag should use default string metavar:\n%s", out)
	}
}
