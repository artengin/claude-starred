package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	content := ""

	for _, line := range lines {
		content += line + "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadTranscriptTakesFirstCwdAndLastTitles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path,
		`{"type":"user","cwd":"/first"}`,
		`{"type":"user","cwd":"/second"}`,
		`{"type":"ai-title","aiTitle":"Auto"}`,
		`{"type":"user","message":{"content":"text with \"custom-title\" inside"}}`,
		`{"type":"custom-title","customTitle":"Old"}`,
		`{"type":"custom-title","customTitle":"New"}`,
	)

	transcript, err := ReadTranscript(path)

	if err != nil {
		t.Fatal(err)
	}

	if transcript != (Transcript{Cwd: "/first", CustomTitle: "New", AITitle: "Auto"}) {
		t.Fatalf("unexpected transcript: %+v", transcript)
	}
}

func TestFindTranscriptAcrossProjects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "projects", "-var-www-app", "abc.jsonl")
	writeLines(t, path, `{}`)

	found, err := FindTranscript("abc")

	if err != nil || found != path {
		t.Fatalf("found %q, %v", found, err)
	}

	if _, err := FindTranscript("missing"); err == nil {
		t.Fatal("expected an error for a missing session")
	}
}

func TestLiveSessionIDs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	for pid, id := range map[int]string{os.Getpid(): "alive", 999999999: "dead"} {
		data, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": id})
		writeLines(t, filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"), string(data))
	}

	ids := LiveSessionIDs()

	if !ids["alive"] || ids["dead"] {
		t.Fatalf("unexpected live ids: %v", ids)
	}
}

func TestProjectRootGroupsWorktreesAndSubdirectories(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	repository := filepath.Join(dir, "repo")
	worktree := filepath.Join(dir, "repo-2")
	git := func(args ...string) {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}

	must(t, os.MkdirAll(filepath.Join(repository, "src"), 0o700))
	git("init", "-q", repository)
	git("-C", repository, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	git("-C", repository, "worktree", "add", "-q", worktree)

	for _, cwd := range []string{repository, filepath.Join(repository, "src"), worktree} {
		if root := ProjectRoot(cwd); !sameDir(root, repository) {
			t.Errorf("ProjectRoot(%s) = %s", cwd, root)
		}
	}

	if root := ProjectRoot(filepath.Join(dir, "missing")); root != filepath.Join(dir, "missing") {
		t.Errorf("missing directory should be its own root, got %s", root)
	}

	bare := filepath.Join(dir, "app", ".bare")
	checkout := filepath.Join(dir, "app", "main")
	git("clone", "-q", "--bare", repository, bare)
	git("-C", bare, "worktree", "add", "-q", checkout)
	must(t, os.MkdirAll(filepath.Join(checkout, "src"), 0o700))

	if root := ProjectRoot(filepath.Join(checkout, "src")); !sameDir(root, checkout) {
		t.Errorf("subdirectory of a bare-repo worktree should map to the worktree, got %s", root)
	}
}

func sameDir(a, b string) bool {
	first, err := os.Stat(a)

	if err != nil {
		return false
	}

	second, err := os.Stat(b)

	return err == nil && os.SameFile(first, second)
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
