package main

import (
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    options
		wantErr string
	}{
		{name: "no arguments reads standard input"},
		{name: "file", args: []string{"data.json"}, want: options{path: "data.json"}},
		{name: "windows path", args: []string{`C:\data\a b.json`}, want: options{path: `C:\data\a b.json`}},
		{name: "short help", args: []string{"-h"}, want: options{help: true}},
		{name: "long help", args: []string{"--help"}, want: options{help: true}},
		{name: "help wins over a file", args: []string{"data.json", "--help"}, want: options{help: true}},
		{name: "short version", args: []string{"-V"}, want: options{version: true}},
		{name: "long version", args: []string{"--version"}, want: options{version: true}},
		{name: "version wins over a file", args: []string{"data.json", "--version"}, want: options{version: true}},
		{name: "unknown option", args: []string{"-r"}, wantErr: "unknown option -r"},
		{name: "lowercase v is not version", args: []string{"-v"}, wantErr: "unknown option -v"},
		{name: "unknown option after a file", args: []string{"data.json", "-r"}, wantErr: "unknown option -r"},
		{name: "too many arguments", args: []string{"a.json", "b.json"}, wantErr: "too many arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		built    string
		want     string
	}{
		{"injected", "1.2.3", "", "1.2.3"},
		{"injected wins over built", "1.2.3", "v0.0.1", "1.2.3"},
		{"injected tag loses its v", "v1.2.3", "", "1.2.3"},
		{"built by go install", "", "v0.4.0", "0.4.0"},
		{"built from a commit", "", "v0.0.0-20261107120000-abcdef123456+dirty", "0.0.0-20261107120000-abcdef123456+dirty"},
		{"unstamped build", "", "(devel)", "dev"},
		{"no build info", "", "", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.injected, tt.built); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
