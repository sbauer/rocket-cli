package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseArchives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release script requires a Unix shell")
	}
	for _, tool := range []string{"bash", "tar", "gzip", "zip", "shasum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("release packaging requires %s: %v", tool, err)
		}
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	binDir := filepath.Join(dir, "bin")
	for _, path := range []string{dist, binDir} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	fakeGo := `#!/bin/sh
set -eu
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    printf '%s/%s/%s\n' "$GOOS" "$GOARCH" "$RELEASE_TEST_PAYLOAD" > "$2"
    exit 0
  fi
  shift
done
printf 'missing build output\n' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(fakeGo), 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dist, "rocket_v0.0.0_linux_amd64")
	if err := os.WriteFile(stale, []byte("unrelated"), 0644); err != nil {
		t.Fatal(err)
	}
	const version = "v1.2.3-beta.1"
	oldArchive := filepath.Join(dist, "rocket_"+version+"_linux_amd64.zip")
	if err := os.WriteFile(oldArchive, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	run := func(payload, timezone string) {
		t.Helper()
		cmd := exec.Command("bash", script, version)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"RELEASE_TEST_PAYLOAD="+payload,
			"TZ="+timezone,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build release: %v\n%s", err, output)
		}
	}
	check := func(payload string) map[string][]byte {
		t.Helper()
		assets := make(map[string][]byte)
		for _, targetOS := range []string{"linux", "darwin", "windows"} {
			for _, arch := range []string{"amd64", "arm64"} {
				name := fmt.Sprintf("rocket_%s_%s_%s", version, targetOS, arch)
				binary := "rocket"
				if targetOS == "windows" {
					binary = "rocket.exe"
				}
				content := fmt.Sprintf("%s/%s/%s\n", targetOS, arch, payload)
				for _, ext := range []string{".tar.gz", ".zip"} {
					asset := name + ext
					data, err := os.ReadFile(filepath.Join(dist, asset))
					if err != nil {
						t.Fatal(err)
					}
					checkReleaseArchive(t, asset, data, binary, content)
					assets[asset] = data
				}
			}
		}
		manifest, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(manifest)), "\n")
		if len(lines) != len(assets) {
			t.Fatalf("manifest has %d entries, want %d", len(lines), len(assets))
		}
		seen := make(map[string]bool)
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				t.Fatalf("invalid checksum line: %q", line)
			}
			data, ok := assets[fields[1]]
			if !ok || seen[fields[1]] {
				t.Fatalf("unexpected or duplicate checksum entry: %q", fields[1])
			}
			sum := sha256.Sum256(data)
			if fields[0] != fmt.Sprintf("%x", sum) {
				t.Fatalf("incorrect checksum for %s", fields[1])
			}
			seen[fields[1]] = true
		}
		entries, err := os.ReadDir(dist)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 14 {
			t.Fatalf("unexpected release directory entries: %v", entries)
		}
		if data, err := os.ReadFile(stale); err != nil || string(data) != "unrelated" {
			t.Fatalf("existing artifact was modified: %q, %v", data, err)
		}
		return assets
	}

	run("first", "America/Los_Angeles")
	check("first")
	run("other", "Pacific/Auckland")
	previous := check("other")
	run("other", "Europe/Berlin")
	for name, data := range check("other") {
		if !bytes.Equal(data, previous[name]) {
			t.Errorf("%s changed between identical builds", name)
		}
	}
}

func checkReleaseArchive(t *testing.T, name string, data []byte, binary, content string) {
	t.Helper()
	var got []byte
	if strings.HasSuffix(name, ".tar.gz") {
		compressed, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reader := tar.NewReader(compressed)
		header, err := reader.Next()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if header.Name != binary || header.Typeflag != tar.TypeReg ||
			header.Mode != 0755 || header.Uid != 0 || header.Gid != 0 {
			t.Fatalf("%s: unexpected tar entry: %+v", name, header)
		}
		got, err = io.ReadAll(reader)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := reader.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("%s: expected exactly one tar entry, got %v", name, err)
		}
		if _, err := io.Copy(io.Discard, compressed); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	} else {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(reader.File) != 1 || reader.File[0].Name != binary || reader.File[0].Mode().Perm() != 0755 {
			t.Fatalf("%s: unexpected zip entries: %+v", name, reader.File)
		}
		file, err := reader.File[0].Open()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err = io.ReadAll(file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if string(got) != content {
		t.Fatalf("%s: content %q, want %q", name, got, content)
	}
}
