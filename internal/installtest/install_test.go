// Package installtest exercises scripts/install.sh end to end with fake
// external commands, so the shell installer is covered by `go test ./...`.
package installtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ghMode selects the behaviour of the fake `gh` placed on the test PATH.
type ghMode string

const (
	ghOK       ghMode = "ok"      // delivers archive and checksums
	ghFail     ghMode = "fail"    // exits non-zero, delivers nothing
	ghMissing  ghMode = "missing" // exits zero but delivers no archive
	ghBadSum   ghMode = "badsum"  // delivers archive with wrong checksums
	binaryName        = "herdr-pr-glance"
	repoSlug          = "cupsadarius/herdr-pr-glance"
)

// checksumTool selects which SHA-256 helper is visible on the test PATH.
type checksumTool int

const (
	bothSums checksumTool = iota
	onlySha256sum
	onlyShasum
)

type fixture struct {
	root       string // plugin root containing VERSION, scripts/, bin/
	pathDir    string // restricted PATH directory
	ghLog      string // file receiving the fake gh argv
	payload    []byte // bytes packed into the archive
	archive    string // expected archive file name
	scriptPath string
}

// repoRoot walks up from the test file to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// tarGz packs payload as the single archive member `name`.
func tarGz(t *testing.T, name string, payload []byte, mode int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: mode, Size: int64(len(payload)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// newFixture builds a plugin root, release fixtures and a restricted PATH.
func newFixture(t *testing.T, baseDir, version, unameS, unameM string, mode ghMode, tool checksumTool) *fixture {
	t.Helper()
	root := filepath.Join(baseDir, "plugin root")
	mustMkdir(t, filepath.Join(root, "scripts"))
	mustMkdir(t, filepath.Join(root, "bin"))
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
	src := filepath.Join(repoRoot(t), "scripts", "install.sh")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read installer %s: %v", src, err)
	}
	scriptPath := filepath.Join(root, "scripts", "install.sh")
	writeExec(t, scriptPath, string(body))

	osName := map[string]string{"Darwin": "darwin", "Linux": "linux"}[unameS]
	archName := map[string]string{"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[unameM]
	archive := fmt.Sprintf("%s_%s_%s_%s.tar.gz", binaryName, version, osName, archName)

	payload := []byte("#!/bin/sh\necho " + osName + "/" + archName + " " + version + "\n")
	assets := filepath.Join(baseDir, "assets")
	mustMkdir(t, assets)
	if osName != "" && archName != "" {
		blob := tarGz(t, binaryName, payload, 0o755)
		if err := os.WriteFile(filepath.Join(assets, archive), blob, 0o644); err != nil {
			t.Fatalf("write archive: %v", err)
		}
		sum := sha256.Sum256(blob)
		if mode == ghBadSum {
			sum = sha256.Sum256([]byte("something else entirely"))
		}
		line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archive)
		if err := os.WriteFile(filepath.Join(assets, "checksums.txt"), []byte(line), 0o644); err != nil {
			t.Fatalf("write checksums: %v", err)
		}
	}

	pathDir := filepath.Join(baseDir, "fake bin")
	mustMkdir(t, pathDir)
	linkRealTools(t, pathDir)
	writeExec(t, filepath.Join(pathDir, "uname"), fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) echo %q ;;
esac
`, unameS, unameM, unameS))

	ghLog := filepath.Join(baseDir, "gh.log")
	writeExec(t, filepath.Join(pathDir, "gh"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
outdir=
prev=
for a in "$@"; do
  if [ "$prev" = "-D" ]; then outdir=$a; fi
  prev=$a
done
case %q in
  fail) echo "fake gh: release not found" >&2; exit 1 ;;
  missing) cp %q/checksums.txt "$outdir"/ 2>/dev/null || true; exit 0 ;;
  *) cp %q/* "$outdir"/ ; exit 0 ;;
esac
`, ghLog, string(mode), assets, assets))

	return &fixture{root: root, pathDir: pathDir, ghLog: ghLog, payload: payload, archive: archive, scriptPath: scriptPath}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// linkRealTools symlinks the POSIX utilities the installer may legitimately use.
func linkRealTools(t *testing.T, pathDir string) {
	t.Helper()
	for _, name := range []string{"tar", "gzip", "mktemp", "mkdir", "rm", "mv", "cp", "chmod", "awk", "cat", "wc", "dirname", "sed", "tr", "sha256sum", "shasum"} {
		real, err := exec.LookPath(name)
		if err != nil {
			continue // optional tool, absent on this host
		}
		_ = os.Symlink(real, filepath.Join(pathDir, name))
	}
	// GNU tar execs a separate gzip binary for -z, so the installer cannot run
	// without it on Linux hosts.
	if _, err := exec.LookPath("gzip"); err != nil {
		t.Skip("gzip is not installed on this host; the installer requires it")
	}
}

func (f *fixture) dropChecksumTool(t *testing.T, tool checksumTool) {
	t.Helper()
	switch tool {
	case onlySha256sum:
		_ = os.Remove(filepath.Join(f.pathDir, "shasum"))
	case onlyShasum:
		_ = os.Remove(filepath.Join(f.pathDir, "sha256sum"))
	}
}

// run executes the installer with only the fake PATH available.
func (f *fixture) run(t *testing.T) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", f.scriptPath)
	cmd.Dir = t.TempDir() // cwd is deliberately unrelated to the plugin root
	cmd.Env = []string{"PATH=" + f.pathDir, "HOME=" + f.root}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *fixture) installed() string { return filepath.Join(f.root, "bin", binaryName) }

func TestInstallAllTargets(t *testing.T) {
	cases := []struct{ unameS, unameM, wantOS, wantArch string }{
		{"Darwin", "x86_64", "darwin", "amd64"},
		{"Darwin", "arm64", "darwin", "arm64"},
		{"Linux", "x86_64", "linux", "amd64"},
		{"Linux", "aarch64", "linux", "arm64"},
	}
	for _, tc := range cases {
		t.Run(tc.wantOS+"_"+tc.wantArch, func(t *testing.T) {
			f := newFixture(t, t.TempDir(), "0.1.0", tc.unameS, tc.unameM, ghOK, bothSums)
			out, err := f.run(t)
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, out)
			}
			got, err := os.ReadFile(f.installed())
			if err != nil {
				t.Fatalf("installed binary: %v\n%s", err, out)
			}
			if !bytes.Equal(got, f.payload) {
				t.Fatalf("installed contents = %q, want %q", got, f.payload)
			}
			log, err := os.ReadFile(f.ghLog)
			if err != nil {
				t.Fatalf("gh log: %v", err)
			}
			want := fmt.Sprintf("%s_0.1.0_%s_%s.tar.gz", binaryName, tc.wantOS, tc.wantArch)
			for _, frag := range []string{"release download", "v0.1.0", repoSlug, want, "checksums.txt"} {
				if !strings.Contains(string(log), frag) {
					t.Fatalf("gh argv %q missing %q", log, frag)
				}
			}
			if strings.Contains(string(log), "latest") {
				t.Fatalf("installer must never use latest: %q", log)
			}
		})
	}
}

func TestInstallSetsExecutableMode(t *testing.T) {
	f := newFixture(t, t.TempDir(), "0.1.0", "Linux", "x86_64", ghOK, bothSums)
	if out, err := f.run(t); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, out)
	}
	info, err := os.Stat(f.installed())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode = %o, want 755", perm)
	}
}

func TestInstallWorksWithSpacesInPaths(t *testing.T) {
	base := filepath.Join(t.TempDir(), "dir with spaces")
	mustMkdir(t, base)
	f := newFixture(t, base, "0.1.0", "Darwin", "arm64", ghOK, bothSums)
	if out, err := f.run(t); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(f.installed()); err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
}

func TestInstallWithEitherChecksumTool(t *testing.T) {
	for name, tool := range map[string]checksumTool{"sha256sum": onlySha256sum, "shasum": onlyShasum} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, t.TempDir(), "0.1.0", "Linux", "x86_64", ghOK, tool)
			f.dropChecksumTool(t, tool)
			if _, err := os.Stat(filepath.Join(f.pathDir, name)); err != nil {
				t.Skipf("%s not available on this host", name)
			}
			out, err := f.run(t)
			if err != nil {
				t.Fatalf("installer failed with only %s: %v\n%s", name, err, out)
			}
			got, err := os.ReadFile(f.installed())
			if err != nil {
				t.Fatalf("installed binary: %v", err)
			}
			if !bytes.Equal(got, f.payload) {
				t.Fatalf("installed contents = %q, want %q", got, f.payload)
			}
		})
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	cases := []struct{ unameS, unameM, want string }{
		{"FreeBSD", "x86_64", "FreeBSD"},
		{"Linux", "i386", "i386"},
	}
	for _, tc := range cases {
		t.Run(tc.unameS+"_"+tc.unameM, func(t *testing.T) {
			f := newFixture(t, t.TempDir(), "0.1.0", tc.unameS, tc.unameM, ghOK, bothSums)
			out, err := f.run(t)
			if err == nil {
				t.Fatalf("expected failure, got success:\n%s", out)
			}
			if !strings.Contains(out, tc.want) || !strings.Contains(strings.ToLower(out), "unsupported") {
				t.Fatalf("error message %q lacks unsupported-platform detail", out)
			}
			if _, statErr := os.Stat(f.installed()); statErr == nil {
				t.Fatalf("binary installed despite unsupported platform")
			}
			if log, _ := os.ReadFile(f.ghLog); len(log) != 0 {
				t.Fatalf("gh invoked for unsupported platform: %q", log)
			}
		})
	}
}

func TestFailureModesPreserveExistingBinary(t *testing.T) {
	cases := []struct {
		name string
		mode ghMode
		want string
	}{
		{"failed download", ghFail, "download"},
		{"missing asset", ghMissing, "missing"},
		{"checksum mismatch", ghBadSum, "checksum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, t.TempDir(), "0.1.0", "Linux", "arm64", tc.mode, bothSums)
			old := []byte("previous binary\n")
			if err := os.WriteFile(f.installed(), old, 0o755); err != nil {
				t.Fatalf("seed old binary: %v", err)
			}
			out, err := f.run(t)
			if err == nil {
				t.Fatalf("expected failure, got success:\n%s", out)
			}
			if !strings.Contains(strings.ToLower(out), tc.want) {
				t.Fatalf("message %q lacks %q", out, tc.want)
			}
			got, readErr := os.ReadFile(f.installed())
			if readErr != nil {
				t.Fatalf("old binary removed: %v", readErr)
			}
			if !bytes.Equal(got, old) {
				t.Fatalf("old binary modified: %q", got)
			}
		})
	}
}

func TestTemporaryFilesCleanedUp(t *testing.T) {
	base := t.TempDir()
	f := newFixture(t, base, "0.1.0", "Linux", "x86_64", ghOK, bothSums)
	tmp := filepath.Join(base, "tmpdir")
	mustMkdir(t, tmp)
	cmd := exec.Command("/bin/sh", f.scriptPath)
	cmd.Dir = base
	cmd.Env = []string{"PATH=" + f.pathDir, "TMPDIR=" + tmp, "HOME=" + f.root}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read tmpdir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
	binEntries, err := os.ReadDir(filepath.Join(f.root, "bin"))
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if len(binEntries) != 1 || binEntries[0].Name() != binaryName {
		t.Fatalf("bin/ contains stray files: %v", binEntries)
	}
}
