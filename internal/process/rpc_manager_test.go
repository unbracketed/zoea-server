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
	}, "./sessions", "")

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
	}, "./sessions", defaultWorkingDir)

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
