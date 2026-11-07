package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	mitText = `MIT License

Copyright (c) 2026 Someone

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software...`
	bsd3Text = `Copyright (c) 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:
   * Redistributions in binary form must reproduce the above copyright notice...
   * Neither the name of Google LLC nor the names of its contributors...`
	bsd2Text   = "Redistributions of source code must retain...\nRedistributions in binary form must reproduce..."
	apacheText = "                                 Apache License\n                           Version 2.0, January 2004"
	iscText    = "Permission to use, copy, modify, and/or distribute this software for any purpose..."
	gplText    = "GNU GENERAL PUBLIC LICENSE\nVersion 3, 29 June 2007"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name, text, want string
		ok               bool
	}{
		{"MIT", mitText, "MIT", true},
		{"MIT wrapped differently", "Permission is hereby granted,\r\n  free of charge, to any person", "MIT", true},
		{"BSD-3-Clause", bsd3Text, "BSD-3-Clause", true},
		{"BSD-2-Clause", bsd2Text, "BSD-2-Clause", true},
		{"Apache-2.0", apacheText, "Apache-2.0", true},
		{"ISC", iscText, "ISC", true},
		{"GPL is not accepted", gplText, "", false},
		{"unknown text", "All rights reserved.", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classify(tt.text)
			if got != tt.want || ok != tt.ok {
				t.Errorf("got %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func moduleDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCollect(t *testing.T) {
	dir := moduleDir(t, map[string]string{
		"LICENSE":   bsd3Text,
		"PATENTS":   "Additional IP Rights Grant",
		"main.go":   "package x",
		"README.md": "MIT",
	})
	n, err := collect(module{Path: "example.com/x", Version: "v1.0.0", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(n.licenses, ","); got != "BSD-3-Clause" {
		t.Errorf("licenses %q", got)
	}
	var names []string
	for _, f := range n.texts {
		names = append(names, f.name)
	}
	if got := strings.Join(names, ","); got != "LICENSE,PATENTS" {
		t.Errorf("files %q, want the license first and then the patent grant", got)
	}
}

func TestCollectFileNames(t *testing.T) {
	for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "License", "LICENCE", "COPYING", "LICENSE-MIT"} {
		t.Run(name, func(t *testing.T) {
			dir := moduleDir(t, map[string]string{name: mitText})
			if _, err := collect(module{Path: "example.com/x", Version: "v1.0.0", Dir: dir}); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestCollectFailures(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no license file", map[string]string{"README.md": "MIT"}, "no license file"},
		{"notice without a license", map[string]string{"NOTICE": "x"}, "no license file"},
		{"unrecognized license", map[string]string{"LICENSE": gplText}, "not a license this tool recognizes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := collect(module{Path: "example.com/x", Version: "v1.0.0", Dir: moduleDir(t, tt.files)})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestOverride(t *testing.T) {
	const path = "github.com/mattn/go-localereader"
	empty := moduleDir(t, map[string]string{"README.md": "MIT"})

	n, err := collect(module{Path: path, Version: "v0.0.1", Dir: empty})
	if err != nil {
		t.Fatal(err)
	}
	if n.licenses[0] != "MIT" || !strings.Contains(n.texts[0].text, "Yasuhiro Matsumoto") || n.reason == "" {
		t.Errorf("override not applied: %+v", n)
	}

	if _, err := collect(module{Path: path, Version: "v0.0.2", Dir: empty}); err == nil || !strings.Contains(err.Error(), "check the new version") {
		t.Errorf("override applied to an unchecked version: %v", err)
	}
}

func TestOverrideIsUnusedOnceTheModuleHasALicense(t *testing.T) {
	dir := moduleDir(t, map[string]string{"LICENSE": mitText})
	n, err := collect(module{Path: "github.com/mattn/go-localereader", Version: "v0.0.1", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if n.reason != "" || !strings.Contains(n.texts[0].text, "Someone") {
		t.Errorf("override used despite a license file: %+v", n)
	}
}

func TestRender(t *testing.T) {
	var b bytes.Buffer
	render(&b, []notice{
		{module: module{Path: "Go", Version: "go1.27.3"}, licenses: []string{"BSD-3-Clause"}, texts: []file{{"LICENSE", "go\r\nlicense\n\n"}}},
		{module: module{Path: "example.com/x", Version: "v1.0.0"}, licenses: []string{"MIT"}, texts: []file{{"LICENSE", "x"}}, reason: "because"},
	})
	out := b.String()
	for _, want := range []string{
		"  Go ", "  example.com/x ",
		"Go go1.27.3 (BSD-3-Clause)", "--- LICENSE\n\ngo\nlicense\n",
		"example.com/x v1.0.0 (MIT)", "Note: because",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\r") {
		t.Error("output has CRLF line endings")
	}
}

// TestRunOnThisTestBinary checks the whole tool against a real binary.
func TestRunOnThisTestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the module cache")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := run(exe, &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Go go1.") {
		t.Errorf("Go's own license is missing:\n%s", b.String())
	}
}
