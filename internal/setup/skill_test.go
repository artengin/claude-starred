package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndRemoveSkill(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	if err := InstallSkill("/opt/bin/starred"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(SkillDir(), "SKILL.md"))

	if err != nil {
		t.Fatal(err)
	}

	for _, expected := range []string{"name: star", "Bash('/opt/bin/starred' info:*), Bash('/opt/bin/starred' star:*)", "`'/opt/bin/starred' info ${CLAUDE_SESSION_ID}`", "<<'STARRED_NAME'"} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("skill does not contain %q", expected)
		}
	}

	if err := RemoveSkill(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(SkillDir()); !os.IsNotExist(err) {
		t.Fatal("skill directory left after removal")
	}
}

func TestForeignSkillIsNotTouched(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(SkillDir(), "SKILL.md")
	must(t, os.MkdirAll(SkillDir(), 0o755))
	must(t, os.WriteFile(path, []byte("someone else's star skill"), 0o644))

	if err := InstallSkill("/opt/bin/starred"); err == nil {
		t.Fatal("expected an error for a foreign skill")
	}

	if err := RemoveSkill(); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(path); string(data) != "someone else's star skill" {
		t.Fatal("foreign skill was changed")
	}
}

func TestVerifyChecksum(t *testing.T) {
	checksums := []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  empty.tar.gz\n")

	if err := verifyChecksum(nil, checksums, "empty.tar.gz"); err != nil {
		t.Fatal(err)
	}

	if err := verifyChecksum([]byte("x"), checksums, "empty.tar.gz"); err == nil {
		t.Fatal("expected a checksum mismatch")
	}

	if err := verifyChecksum(nil, checksums, "other.tar.gz"); err == nil {
		t.Fatal("expected a missing checksum error")
	}
}

func TestSkillUsesTheBareNameWhenTheBinaryIsOnPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	bin := t.TempDir()
	executable := filepath.Join(bin, binaryName())
	must(t, os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", bin)

	command, err := skillCommand(executable)
	must(t, err)

	if command != binaryName() {
		t.Fatalf("expected the bare name, got %s", command)
	}

	if _, err := skillCommand("/opt/tools (x86)/starred"); err == nil {
		t.Fatal("a path with parentheses must be rejected when it is not on PATH")
	}

	if command, err := skillCommand(filepath.Join(t.TempDir(), "bin", "starred")); err != nil || !strings.HasPrefix(command, "'") {
		t.Fatalf("a plain path off PATH must be quoted, got %q, %v", command, err)
	}
}

func TestShellQuote(t *testing.T) {
	for path, expected := range map[string]string{
		"/opt/bin/starred":         "'/opt/bin/starred'",
		"/home/my user/starred":    "'/home/my user/starred'",
		"/home/o'neil/bin/starred": `'/home/o'\''neil/bin/starred'`,
	} {
		if quoted := shellQuote(path); quoted != expected {
			t.Errorf("shellQuote(%q) = %s", path, quoted)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
