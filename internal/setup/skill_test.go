package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndRemoveSkills(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	if installed, skipped, err := InstallSkills("/opt/bin/claude-starred"); err != nil || len(installed) != 2 || len(skipped) != 0 {
		t.Fatalf("expected both skills installed, got %v, %v, %v", installed, skipped, err)
	}

	expectations := map[string][]string{
		"star":   {"name: star", "Bash('/opt/bin/claude-starred' info:*), Bash('/opt/bin/claude-starred' star:*)", "`'/opt/bin/claude-starred' info ${CLAUDE_SESSION_ID}`", "<<'STARRED_NAME'"},
		"unstar": {"name: unstar", "allowed-tools: Bash('/opt/bin/claude-starred' unstar:*)", "'/opt/bin/claude-starred' unstar ${CLAUDE_SESSION_ID}"},
	}

	for _, s := range skills {
		data, err := os.ReadFile(s.file())

		if err != nil {
			t.Fatal(err)
		}

		for _, text := range append(expectations[s.name], skillMarker) {
			if !strings.Contains(string(data), text) {
				t.Errorf("%s skill does not contain %q", s.name, text)
			}
		}
	}

	if err := RemoveSkills(); err != nil {
		t.Fatal(err)
	}

	for _, s := range skills {
		if _, err := os.Stat(filepath.Dir(s.file())); !os.IsNotExist(err) {
			t.Fatalf("%s skill directory left after removal", s.name)
		}
	}
}

func TestForeignUnstarSkillIsSkipped(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	star, unstar := skills[0], skills[1]
	must(t, os.MkdirAll(filepath.Dir(unstar.file()), 0o755))
	must(t, os.WriteFile(unstar.file(), []byte("my wrapper around claude-starred unstar"), 0o644))

	installed, skipped, err := InstallSkills("/opt/bin/claude-starred")

	if err != nil || len(installed) != 1 || installed[0] != "star" || len(skipped) != 1 || skipped[0] != unstar.file() {
		t.Fatalf("expected only /star installed and /unstar skipped, got %v, %v, %v", installed, skipped, err)
	}

	must(t, RemoveSkills())

	if _, err := os.Stat(filepath.Dir(star.file())); !os.IsNotExist(err) {
		t.Fatal("our /star left after removal")
	}

	if data, _ := os.ReadFile(unstar.file()); string(data) != "my wrapper around claude-starred unstar" {
		t.Fatal("foreign skill was changed")
	}
}

func TestForeignStarSkillStopsTheInstall(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	star, unstar := skills[0], skills[1]
	must(t, os.MkdirAll(filepath.Dir(star.file()), 0o755))
	must(t, os.WriteFile(star.file(), []byte("someone else's star skill"), 0o644))

	if _, _, err := InstallSkills("/opt/bin/claude-starred"); err == nil {
		t.Fatal("expected an error for a foreign /star")
	}

	if _, err := os.Stat(unstar.file()); !os.IsNotExist(err) {
		t.Fatal("/unstar must not be installed without /star")
	}

	if SkillInstalled() {
		t.Fatal("a foreign skill is taken for ours")
	}

	must(t, RemoveSkills())

	if data, _ := os.ReadFile(star.file()); string(data) != "someone else's star skill" {
		t.Fatal("foreign skill was changed")
	}
}

func TestLegacyAndEditedSkillsAreStillOurs(t *testing.T) {
	cases := map[string]string{
		"legacy":  "---\nname: star\n" + legacyStarDescription + "\nallowed-tools: Bash(starred info:*)\n---\n\nold body\n",
		"crlf":    "---\r\nname: star\r\n" + legacyStarDescription + "\r\n---\r\n\r\n" + skillMarker + "\r\n",
		"wrapper": "---\nname: star\ndescription: My own star that calls claude-starred under the hood.\n---\n\nRun claude-starred star\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
			star := skills[0]
			must(t, os.MkdirAll(filepath.Dir(star.file()), 0o755))
			must(t, os.WriteFile(star.file(), []byte(content), 0o644))

			state, err := star.state()
			must(t, err)

			if own := state == ownSkill; own == (name == "wrapper") {
				t.Fatalf("%s skill classified as own=%v", name, own)
			}
		})
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

	if _, err := skillCommand("/opt/tools (x86)/claude-starred"); err == nil {
		t.Fatal("a path with parentheses must be rejected when it is not on PATH")
	}

	if command, err := skillCommand(filepath.Join(t.TempDir(), "bin", "claude-starred")); err != nil || !strings.HasPrefix(command, "'") {
		t.Fatalf("a plain path off PATH must be quoted, got %q, %v", command, err)
	}
}

func TestShellQuote(t *testing.T) {
	for path, expected := range map[string]string{
		"/opt/bin/claude-starred":         "'/opt/bin/claude-starred'",
		"/home/my user/claude-starred":    "'/home/my user/claude-starred'",
		"/home/o'neil/bin/claude-starred": `'/home/o'\''neil/bin/claude-starred'`,
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
