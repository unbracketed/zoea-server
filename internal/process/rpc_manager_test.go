package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRPCProcessManagerUsesWorkingDirAndAbsoluteSessionDir(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	workingDir := filepath.Join(root, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}

	pwdFile := filepath.Join(root, "pwd.txt")
	argsFile := filepath.Join(root, "args.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`printf '%s\n' "$PWD" > "$1"; args_file="$2"; shift 2; printf '%s\n' "$@" > "$args_file"`,
		"sh",
		pwdFile,
		argsFile,
	}, "./sessions", "", "")

	_, err = pm.Start(context.Background(), StartOptions{SessionID: "s1", UserID: "u1", WorkingDir: workingDir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	pwd := strings.TrimSpace(waitForFileString(t, pwdFile))
	if pwd != workingDir {
		t.Fatalf("pwd: got %q want %q", pwd, workingDir)
	}

	args := splitNonEmptyLines(waitForFileString(t, argsFile))
	if len(args) != 2 {
		t.Fatalf("args: got %v", args)
	}
	if args[0] != "--session-dir" {
		t.Fatalf("first arg: got %q want --session-dir", args[0])
	}
	if !filepath.IsAbs(args[1]) {
		t.Fatalf("session dir should be absolute, got %q", args[1])
	}
	wantSessionDir := filepath.Join(root, "sessions", "u1", "s1")
	gotSessionDir, err := filepath.EvalSymlinks(args[1])
	if err != nil {
		t.Fatalf("eval symlinks on got session dir: %v", err)
	}
	wantSessionDir, err = filepath.EvalSymlinks(wantSessionDir)
	if err != nil {
		t.Fatalf("eval symlinks on wanted session dir: %v", err)
	}
	if gotSessionDir != wantSessionDir {
		t.Fatalf("session dir: got %q want %q", gotSessionDir, wantSessionDir)
	}
}

func TestRPCProcessManagerDefaultWorkingDirOverridesRequestWorkingDir(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	defaultWorkingDir := filepath.Join(root, "default-project")
	requestWorkingDir := filepath.Join(root, "request-project")
	for _, dir := range []string{defaultWorkingDir, requestWorkingDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	pwdFile := filepath.Join(root, "pwd.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`printf '%s\n' "$PWD" > "$1"`,
		"sh",
		pwdFile,
	}, "./sessions", defaultWorkingDir, "")

	_, err = pm.Start(context.Background(), StartOptions{SessionID: "s1", UserID: "u1", WorkingDir: requestWorkingDir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	pwd := strings.TrimSpace(waitForFileString(t, pwdFile))
	if pwd != defaultWorkingDir {
		t.Fatalf("pwd: got %q want %q", pwd, defaultWorkingDir)
	}
}

func waitForFileString(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("read %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForFileContaining is like waitForFileString but keeps polling
// until the file contains a specific substring. Useful when the
// subprocess writes its output after a small startup delay so the
// first read would catch an empty or partial file.
func waitForFileContaining(t *testing.T, path, needle string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), needle) {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s never contained %q (last err: %v, last content: %q)",
				path, needle, err, string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func splitNonEmptyLines(s string) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// TestRPCProcessManagerAddsContinueWhenSessionDirHasTranscript covers the
// resume path: a session-dir already containing a Pi .jsonl must trigger
// --continue so Pi loads the prior conversation. A fresh dir must not.
func TestRPCProcessManagerAddsContinueWhenSessionDirHasTranscript(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	workingDir := filepath.Join(root, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}
	sessionsBase := filepath.Join(root, "sessions")
	sessionDir := filepath.Join(sessionsBase, "u1", "s1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	// Pre-seed a Pi-style transcript.
	if err := os.WriteFile(filepath.Join(sessionDir, "2026-01-01T00-00-00-000Z_abc.jsonl"), []byte(`{"type":"session","id":"abc"}`+"\n"), 0o644); err != nil {
		t.Fatalf("seed jsonl: %v", err)
	}

	argsFile := filepath.Join(root, "args.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`args_file="$1"; shift; printf '%s\n' "$@" > "$args_file"`,
		"sh",
		argsFile,
	}, sessionsBase, "", "")

	_, err = pm.Start(context.Background(), StartOptions{SessionID: "s1", UserID: "u1", WorkingDir: workingDir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	args := splitNonEmptyLines(waitForFileString(t, argsFile))
	if !containsString(args, "--continue") {
		t.Fatalf("expected --continue in args when session-dir has transcript, got %v", args)
	}
}

func TestRPCProcessManagerOmitsContinueForFreshSessionDir(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	workingDir := filepath.Join(root, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}

	argsFile := filepath.Join(root, "args.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`args_file="$1"; shift; printf '%s\n' "$@" > "$args_file"`,
		"sh",
		argsFile,
	}, filepath.Join(root, "sessions"), "", "")

	_, err = pm.Start(context.Background(), StartOptions{SessionID: "s1", UserID: "u1", WorkingDir: workingDir})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	args := splitNonEmptyLines(waitForFileString(t, argsFile))
	if containsString(args, "--continue") {
		t.Fatalf("did not expect --continue for fresh session-dir, got %v", args)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestRPCProcessManagerInjectsBasilA2UIEnv(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	workingDir := filepath.Join(root, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	envFile := filepath.Join(root, "env.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`env > "$1"`,
		"sh",
		envFile,
	}, "./sessions", "", "http://zoea.local:14004")

	_, err = pm.Start(context.Background(), StartOptions{
		SessionID: "s_unit",
		UserID:    "u1",
		WorkingDir: workingDir,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	envText := waitForFileContaining(t, envFile, "BASIL_A2UI_TRANSPORT=zoea")

	for _, want := range []string{
		"BASIL_A2UI_TRANSPORT=zoea",
		"BASIL_ZOEA_URL=http://zoea.local:14004",
		"BASIL_ZOEA_SESSION_ID=s_unit",
	} {
		if !strings.Contains(envText, want) {
			t.Errorf("env missing %q\nfull env:\n%s", want, envText)
		}
	}
}

func TestRPCProcessManagerSkipsBasilEnvWhenPublicURLEmpty(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	workingDir := filepath.Join(root, "project")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	envFile := filepath.Join(root, "env.txt")
	pm := NewRPCProcessManager("sh", []string{
		"-c",
		`env > "$1"`,
		"sh",
		envFile,
	}, "./sessions", "", "")

	_, err = pm.Start(context.Background(), StartOptions{
		SessionID: "s_unit",
		UserID:    "u1",
		WorkingDir: workingDir,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	envText := waitForFileContaining(t, envFile, "BASIL_A2UI_TRANSPORT=zoea")
	// Transport default still injected; URL skipped because we have nowhere
	// to point it. Session id always tracks the live process.
	if !strings.Contains(envText, "BASIL_A2UI_TRANSPORT=zoea") {
		t.Errorf("expected BASIL_A2UI_TRANSPORT=zoea even without publicURL\n%s", envText)
	}
	if strings.Contains(envText, "BASIL_ZOEA_URL=") {
		t.Errorf("BASIL_ZOEA_URL should be absent when publicURL empty\n%s", envText)
	}
	if !strings.Contains(envText, "BASIL_ZOEA_SESSION_ID=s_unit") {
		t.Errorf("expected session id\n%s", envText)
	}
}
