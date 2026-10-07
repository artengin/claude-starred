package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artengin/claude-starred/internal/store"
)

func TestUnstarRemovesAStarredSession(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "abc.jsonl")
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	s, err := store.Open(filepath.Join(dir, "data"))
	must(t, err)
	must(t, s.Star(store.Record{ID: "abc", Name: "First", Transcript: transcript}))

	must(t, unstar(s, []string{"abc"}))

	if s.Find("abc") != nil {
		t.Fatal("session is still starred")
	}

	if _, err := os.Stat(transcript); err != nil {
		t.Fatal("unstar must not touch the Claude transcript")
	}
}

func TestUnstarReturnsADeletedTranscriptToClaude(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "projects", "abc.jsonl")
	must(t, os.MkdirAll(filepath.Dir(transcript), 0o700))
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	s, err := store.Open(filepath.Join(dir, "data"))
	must(t, err)
	must(t, s.Star(store.Record{ID: "abc", Name: "First", Transcript: transcript}))
	must(t, os.Remove(transcript))

	must(t, unstar(s, []string{"abc"}))

	if _, err := os.Stat(transcript); err != nil || s.Find("abc") != nil {
		t.Fatal("the deleted transcript must be back in Claude and the session unstarred")
	}
}

func TestUnstarExplainsHowToDropACopyThatCannotGoBack(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "projects", "abc.jsonl")
	must(t, os.MkdirAll(filepath.Dir(transcript), 0o700))
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	s, err := store.Open(filepath.Join(dir, "data"))
	must(t, err)
	must(t, s.Star(store.Record{ID: "abc", Name: "First", Transcript: transcript}))
	must(t, os.RemoveAll(filepath.Dir(transcript)))
	must(t, os.WriteFile(filepath.Dir(transcript), nil, 0o600))

	err = unstar(s, []string{"abc"})

	if err == nil || !strings.Contains(err.Error(), "drop the last copy") || s.Find("abc") == nil {
		t.Fatalf("expected the hint about the last copy and an untouched record, got %v", err)
	}
}

func TestUnstarRejectsASessionThatIsNotStarred(t *testing.T) {
	s, err := store.Open(t.TempDir())
	must(t, err)

	if err := unstar(s, []string{"missing"}); err == nil {
		t.Fatal("expected an error for a session that is not starred")
	}
}

func TestMain(m *testing.M) {
	os.Setenv("LC_ALL", "")
	os.Setenv("LC_MESSAGES", "")
	os.Setenv("LANG", "en_US.UTF-8")
	os.Exit(m.Run())
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
