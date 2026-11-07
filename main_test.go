package main

import (
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantHelp bool
		wantErr  string
	}{
		{name: "no arguments reads standard input"},
		{name: "file", args: []string{"data.json"}, wantPath: "data.json"},
		{name: "windows path", args: []string{`C:\data\a b.json`}, wantPath: `C:\data\a b.json`},
		{name: "short help", args: []string{"-h"}, wantHelp: true},
		{name: "long help", args: []string{"--help"}, wantHelp: true},
		{name: "help wins over a file", args: []string{"data.json", "--help"}, wantHelp: true},
		{name: "unknown option", args: []string{"-r"}, wantErr: "unknown option -r"},
		{name: "unknown option after a file", args: []string{"data.json", "-r"}, wantErr: "unknown option -r"},
		{name: "too many arguments", args: []string{"a.json", "b.json"}, wantErr: "too many arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, help, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if path != tt.wantPath || help != tt.wantHelp {
				t.Errorf("got path %q, help %v; want path %q, help %v", path, help, tt.wantPath, tt.wantHelp)
			}
		})
	}
}
