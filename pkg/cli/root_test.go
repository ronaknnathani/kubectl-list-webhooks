package cli

import (
	"bytes"
	"testing"
)

func TestNewCommand(t *testing.T) {
	build := BuildInfo{Version: "v9.9.9", Commit: "abc", Date: "d"}
	cases := []struct {
		name    string
		args    []string
		wantOut string
		wantErr string
	}{
		{
			name:    "version",
			args:    []string{"--version"},
			wantOut: "kubectl list-webhooks version v9.9.9 (commit: abc, date: d)\n",
		},
		{
			name:    "positional args rejected",
			args:    []string{"foo"},
			wantErr: `unknown command "foo" for "kubectl list-webhooks"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newCommand(build)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if gotErr != tc.wantErr {
				t.Errorf("error = %q, want %q", gotErr, tc.wantErr)
			}
			if got := stdout.String(); got != tc.wantOut {
				t.Errorf("stdout = %q, want %q", got, tc.wantOut)
			}
		})
	}
}
