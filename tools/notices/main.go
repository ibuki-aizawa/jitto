// Command notices writes the license texts of everything compiled into a jitto
// binary, so that releases can ship them alongside the binary:
//
//	go run ./tools/notices jitto.exe > THIRD_PARTY_NOTICES.txt
//
// It fails if a module has no license file, or one it does not recognize as
// compatible with redistribution under jitto's MIT license.
package main

import (
	"bufio"
	"bytes"
	"debug/buildinfo"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
)

//go:embed overrides/go-localereader.LICENSE
var localereaderLicense string

// override supplies a license for a module version whose files lack one.
type override struct {
	version string
	text    string
	reason  string
}

// overrides apply to exact versions only. A new version must be checked again.
var overrides = map[string]override{
	"github.com/mattn/go-localereader": {
		version: "v0.0.1",
		text:    localereaderLicense,
		reason: "v0.0.1 has no license file, but its README says MIT. The author later added this " +
			"LICENSE upstream (commit 2491eb6), which differs from v0.0.1 only by that file.",
	},
}

// module is one component of the binary.
type module struct {
	Path    string
	Version string
	Dir     string
}

// notice is the license information of one module.
type notice struct {
	module
	licenses []string // license names, such as "MIT"
	texts    []file   // license, notice and patent files, in order
	reason   string   // why an override was used, if one was
}

type file struct {
	name string
	text string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/notices BINARY > THIRD_PARTY_NOTICES.txt")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run(binary string, w io.Writer) error {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return err
	}
	modules, err := locate(info.Deps)
	if err != nil {
		return err
	}
	goroot, err := goEnv("GOROOT")
	if err != nil {
		return err
	}
	modules = append([]module{{Path: "Go", Version: info.GoVersion, Dir: goroot}}, modules...)

	var notices []notice
	var problems []error
	for _, m := range modules {
		n, err := collect(m)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		notices = append(notices, n)
	}
	if err := errors.Join(problems...); err != nil {
		return err
	}

	out := bufio.NewWriter(w)
	render(out, notices)
	return out.Flush()
}

// locate finds the source directory of each dependency in the module cache,
// downloading it if needed.
func locate(deps []*debug.Module) ([]module, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	args := []string{"mod", "download", "-json"}
	for _, d := range deps {
		if d.Replace != nil {
			d = d.Replace
		}
		if d.Version == "" {
			return nil, fmt.Errorf("%s is replaced by a local directory; its license cannot be checked", d.Path)
		}
		args = append(args, d.Path+"@"+d.Version)
	}
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go mod download: %w", err)
	}

	var modules []module
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var m struct {
			module
			Error string
		}
		if err := dec.Decode(&m); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if m.Error != "" {
			return nil, fmt.Errorf("%s@%s: %s", m.Path, m.Version, m.Error)
		}
		modules = append(modules, m.module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}
	return strings.TrimSpace(string(out)), nil
}

var (
	licenseFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying)([.-].*)?$`)
	extraFile   = regexp.MustCompile(`(?i)^(notice|patents)([.-].*)?$`)
)

// collect reads the license files at the root of m.Dir.
func collect(m module) (notice, error) {
	n := notice{module: m}
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return n, err
	}
	var extras []file
	for _, e := range entries {
		if e.IsDir() || !(licenseFile.MatchString(e.Name()) || extraFile.MatchString(e.Name())) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.Dir, e.Name()))
		if err != nil {
			return n, err
		}
		f := file{e.Name(), string(b)}
		if !licenseFile.MatchString(e.Name()) {
			extras = append(extras, f)
			continue
		}
		name, ok := classify(f.text)
		if !ok {
			return n, fmt.Errorf("%s@%s: %s is not a license this tool recognizes; review it by hand", m.Path, m.Version, e.Name())
		}
		n.licenses = append(n.licenses, name)
		n.texts = append(n.texts, f)
	}

	if len(n.texts) == 0 {
		o, ok := overrides[m.Path]
		switch {
		case !ok:
			return n, fmt.Errorf("%s@%s: no license file", m.Path, m.Version)
		case o.version != m.Version:
			return n, fmt.Errorf("%s@%s: no license file, and the override covers only %s; check the new version", m.Path, m.Version, o.version)
		}
		name, ok := classify(o.text)
		if !ok {
			return n, fmt.Errorf("%s: override text is not a recognized license", m.Path)
		}
		n.licenses = []string{name}
		n.texts = []file{{"LICENSE", o.text}}
		n.reason = o.reason
	}
	n.texts = append(n.texts, extras...)
	return n, nil
}

// classify names a license text. It recognizes only permissive licenses that
// allow binary redistribution with the notice attached.
func classify(text string) (string, bool) {
	t := strings.Join(strings.Fields(text), " ")
	switch {
	case strings.Contains(t, "Apache License") && strings.Contains(t, "Version 2.0"):
		return "Apache-2.0", true
	case strings.Contains(t, "Permission is hereby granted, free of charge"):
		return "MIT", true
	case strings.Contains(t, "Redistributions in binary form") && strings.Contains(t, "Neither the name"):
		return "BSD-3-Clause", true
	case strings.Contains(t, "Redistributions in binary form"):
		return "BSD-2-Clause", true
	case strings.Contains(t, "Permission to use, copy, modify, and/or distribute"),
		strings.Contains(t, "Permission to use, copy, modify, and distribute"):
		return "ISC", true
	}
	return "", false
}

func render(w io.Writer, notices []notice) {
	fmt.Fprintln(w, "jitto includes the following software. Their licenses follow.")
	fmt.Fprintln(w)
	for _, n := range notices {
		fmt.Fprintf(w, "  %-45s %-38s %s\n", n.Path, n.Version, strings.Join(n.licenses, ", "))
	}
	for _, n := range notices {
		fmt.Fprintf(w, "\n%s\n%s %s (%s)\n%s\n", strings.Repeat("=", 80), n.Path, n.Version, strings.Join(n.licenses, ", "), strings.Repeat("=", 80))
		if n.reason != "" {
			fmt.Fprintf(w, "\nNote: %s\n", n.reason)
		}
		for _, f := range n.texts {
			fmt.Fprintf(w, "\n--- %s\n\n%s\n", f.name, strings.TrimRight(strings.ReplaceAll(f.text, "\r\n", "\n"), "\n"))
		}
	}
}
