package cli

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sbauer/go-rocket/parsers"
)

func copyLibraryReplay(t *testing.T, dir, name string, modified time.Time) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "territory.replay"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

func runLibrary(t *testing.T, code int, args ...string) (string, string) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	if got := Run(args, &out, &diagnostics); got != code {
		t.Fatalf("Run(%v) = %d, want %d; stdout %s; stderr %s", args, got, code, &out, &diagnostics)
	}
	return out.String(), diagnostics.String()
}

func TestLibraryListAndSelection(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	old := copyLibraryReplay(t, dir, "old.replay", now.Add(-time.Hour))
	newest := copyLibraryReplay(t, dir, "new.REPLAY", now)
	tied := copyLibraryReplay(t, dir, "tie.replay", now)
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir.replay"), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(newest)
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostics := runLibrary(t, 0, "replays", "list", "--dir", dir)
	if diagnostics != "" || !strings.Contains(out, "UF_Day_P") ||
		!strings.Contains(out, "Private") || !strings.Contains(out, "unknown") ||
		strings.Index(out, "new.REPLAY") > strings.Index(out, "tie.replay") ||
		strings.Index(out, "tie.replay") > strings.Index(out, "old.replay") ||
		strings.Contains(out, "ignored") || strings.Contains(out, "subdir") {
		t.Fatalf("unexpected list: %s; diagnostics %s", out, diagnostics)
	}
	for selector, want := range map[string]string{
		"latest": newest, "1": newest, "2": tied, "3": old, "old": old, "new.REPLAY": newest,
	} {
		got, err := resolveReplay(selector, dir, parsers.CRCValidationAlways, &bytes.Buffer{})
		if err != nil || got != want {
			t.Errorf("resolve %q = %q, %v; want %q", selector, got, err, want)
		}
		output, diagnostics := runLibrary(t, 0, "summary", "--dir", dir, selector)
		if diagnostics != "" || !strings.Contains(output, "Map: UF_Day_P") {
			t.Errorf("summary %q: %s; %s", selector, output, diagnostics)
		}
	}
	t.Setenv("ROCKET_REPLAY_DIR", dir)
	runLibrary(t, 0, "summary", "--format", "json", "latest")
	after, err := os.ReadFile(newest)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(newest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !info.ModTime().Equal(now) {
		t.Fatal("library commands modified replay contents or modification time")
	}
}

func TestLibraryDiagnostics(t *testing.T) {
	dir := t.TempDir()
	copyLibraryReplay(t, dir, "good.replay", time.Now())
	bad := filepath.Join(dir, "bad.replay")
	if err := os.WriteFile(bad, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken.replay")); err != nil {
			t.Fatal(err)
		}
	}
	out, diagnostics := runLibrary(t, 0, "replays", "list", "--dir", dir)
	if !strings.Contains(out, "good.replay") || !strings.Contains(out, "bad.replay") ||
		!strings.Contains(out, "unreadable metadata") || !strings.Contains(diagnostics, bad) {
		t.Fatalf("bad replay hid valid rows or diagnostics: %s; %s", out, diagnostics)
	}
	if runtime.GOOS != "windows" && (!strings.Contains(out, "broken.replay") || !strings.Contains(diagnostics, "broken.replay")) {
		t.Fatalf("broken symlink hid other rows or its error: %s; %s", out, diagnostics)
	}
	for _, selector := range []string{"0", "-1", "4", "missing"} {
		_, diagnostics := runLibrary(t, 1, "summary", "--dir", dir, "--", selector)
		if !strings.Contains(diagnostics, selector) {
			t.Fatalf("missing selector in diagnostic: %s", diagnostics)
		}
	}
	runLibrary(t, 1, "summary", "--dir", dir, "bad.replay")
	runLibrary(t, 1, "replays", "list", "--dir", bad)
	runLibrary(t, 1, "replays", "list", "--dir", filepath.Join(dir, "missing"))
	for _, args := range [][]string{
		{"replays"}, {"replays", "unknown"}, {"replays", "list", "extra"},
		{"replays", "list", "--crc", "invalid"}, {"replays", "list", "--unknown"},
	} {
		runLibrary(t, 2, args...)
	}
	for _, args := range [][]string{{"replays", "--help"}, {"replays", "list", "--help"}} {
		runLibrary(t, 0, args...)
	}
	if err := replays([]string{"list", "--dir", dir}, failingWriter{}, &bytes.Buffer{}); !errors.Is(err, errWrite) {
		t.Fatalf("list write error = %v", err)
	}
}

func TestLibraryListRejectsOversizedReplay(t *testing.T) {
	dir := t.TempDir()
	copyLibraryReplay(t, dir, "good.replay", time.Now())
	oversized := filepath.Join(dir, "oversized.replay")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxReplayInputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	out, diagnostics := runLibrary(t, 0, "replays", "list", "--dir", dir)
	if !strings.Contains(out, "good.replay") || !strings.Contains(out, "oversized.replay") ||
		!strings.Contains(out, "unreadable metadata") ||
		!strings.Contains(diagnostics, oversized) || !strings.Contains(diagnostics, "64 MiB limit") {
		t.Fatalf("oversized replay hid valid rows or lacked limit diagnostic: %s; %s", out, diagnostics)
	}
}

func TestEmptyLibraryAndOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ROCKET_REPLAY_DIR", filepath.Join(dir, "missing"))
	out, diagnostics := runLibrary(t, 0, "replays", "list", "--dir", dir)
	if !strings.Contains(out, "No replays found") || diagnostics != "" {
		t.Fatalf("empty library: %s; %s", out, diagnostics)
	}
	runLibrary(t, 1, "summary", "--dir", dir, "latest")
	runLibrary(t, 1, "replays", "list")
	t.Setenv("ROCKET_REPLAY_DIR", dir)
	got, err := replayDirectory("")
	if err != nil || got != dir {
		t.Fatalf("environment directory = %q, %v", got, err)
	}
	// An explicit local path must work even with a nonexistent library override.
	runLibrary(t, 0, "summary", "--dir", filepath.Join(dir, "missing"), filepath.Join("testdata", "territory.replay"))
}

func TestLocalSelectorPrecedence(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"latest", "3", "match.replay"} {
		copyLibraryReplay(t, dir, name, time.Now())
	}
	t.Chdir(dir)
	for _, name := range []string{"latest", "3", "match.replay"} {
		got, err := resolveReplay(name, "missing-library", parsers.CRCValidationAlways, &bytes.Buffer{})
		if err != nil || got != name {
			t.Fatalf("local selector %q resolved to %q, %v", name, got, err)
		}
	}
}

func TestBackslashReplayFilenameSelector(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	dir := t.TempDir()
	const name = `back\slash.replay`
	path := copyLibraryReplay(t, dir, name, time.Now())

	out, diagnostics := runLibrary(t, 0, "replays", "list", "--dir", dir)
	if diagnostics != "" || !strings.Contains(out, text(name)) {
		t.Fatalf("replay filename missing from list: %s; %s", out, diagnostics)
	}
	got, err := resolveReplay(name, dir, parsers.CRCValidationAlways, &bytes.Buffer{})
	if err != nil || got != path {
		t.Fatalf("resolve %q = %q, %v; want %q", name, got, err, path)
	}
	output, diagnostics := runLibrary(t, 0, "summary", "--dir", dir, name)
	if diagnostics != "" || !strings.Contains(output, "Map: UF_Day_P") {
		t.Fatalf("summary %q: %s; %s", name, output, diagnostics)
	}
}

func TestReplayNamesAndAmbiguity(t *testing.T) {
	dir := t.TempDir()
	path := copyLibraryReplay(t, dir, "one.replay", time.Now())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	headerSize := int(binary.LittleEndian.Uint32(data[:4]))
	var property bytes.Buffer
	writeString := func(value string) {
		t.Helper()
		if err := binary.Write(&property, binary.LittleEndian, int32(len(value)+1)); err != nil {
			t.Fatal(err)
		}
		property.WriteString(value)
		property.WriteByte(0)
	}
	writeString("ReplayName")
	writeString("StrProperty")
	if err := binary.Write(&property, binary.LittleEndian, [2]uint32{0, 0}); err != nil {
		t.Fatal(err)
	}
	writeString("My match")
	header := append(bytes.Clone(data[8:8+headerSize-9]), property.Bytes()...)
	header = append(header, data[8+headerSize-9:8+headerSize]...)
	modified := append(bytes.Clone(data[:8]), header...)
	modified = append(modified, data[8+headerSize:]...)
	binary.LittleEndian.PutUint32(modified[:4], uint32(len(header)))
	if err := os.WriteFile(path, modified, 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostics := runLibrary(t, 0, "replays", "list", "--crc", "never", "--dir", dir)
	if !strings.Contains(out, "My match") || diagnostics != "" {
		t.Fatalf("replay name missing: %s; %s", out, diagnostics)
	}
	runLibrary(t, 0, "summary", "--crc", "never", "--dir", dir, "My match")
	if err := os.WriteFile(filepath.Join(dir, "two.replay"), modified, 0600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = runLibrary(t, 1, "summary", "--crc", "never", "--dir", dir, "My match")
	if !strings.Contains(diagnostics, "ambiguous") || !strings.Contains(diagnostics, "one.replay") ||
		!strings.Contains(diagnostics, "two.replay") {
		t.Fatalf("missing ambiguity detail: %s", diagnostics)
	}
}

func TestChooseReplayDirectory(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	canonical, canonicalErr := filepath.EvalSymlinks(dir)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	got, err := chooseReplayDirectory([]string{dir, dir, filepath.Join(dir, "missing")})
	if err != nil || got != canonical {
		t.Fatalf("deduplicated directory = %q, %v", got, err)
	}
	if _, err := chooseReplayDirectory([]string{dir, other}); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("multiple directory error = %v", err)
	}
	if _, err := chooseReplayDirectory(nil); err == nil || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("missing directory error = %v", err)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(other, "link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if _, err := chooseReplayDirectory([]string{dir, link}); err != nil {
			t.Fatalf("same library via symlink: %v", err)
		}
	}
}
