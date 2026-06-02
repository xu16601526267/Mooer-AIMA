package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUsableKubectlPathRejectsCurrentExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test is unix-specific")
	}

	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	kubectl := filepath.Join(dir, "kubectl")
	if err := os.Symlink(exe, kubectl); err != nil {
		t.Fatalf("symlink kubectl: %v", err)
	}
	t.Setenv("PATH", dir)

	path, err := usableKubectlPath()
	if err == nil {
		t.Fatalf("usableKubectlPath() = %q, nil; want self-reference error", path)
	}
	if path != kubectl {
		t.Fatalf("usableKubectlPath path = %q, want %q", path, kubectl)
	}
}

func TestUsableKubectlPathAcceptsSeparateExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable mode test is unix-specific")
	}

	dir := t.TempDir()
	kubectl := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write kubectl: %v", err)
	}
	t.Setenv("PATH", dir)

	path, err := usableKubectlPath()
	if err != nil {
		t.Fatalf("usableKubectlPath: %v", err)
	}
	if path != kubectl {
		t.Fatalf("usableKubectlPath path = %q, want %q", path, kubectl)
	}
}
