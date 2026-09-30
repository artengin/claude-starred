package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTranscript(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "claude", "projects", "-work", "abc.jsonl")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(`{"type":"user","cwd":"/work"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func starred(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	transcript := newTranscript(t, dir)
	s, err := Open(filepath.Join(dir, "data"))

	if err != nil {
		t.Fatal(err)
	}

	if err := s.Star(Record{ID: "abc", Name: "First", Cwd: "/work", Project: "/work", Transcript: transcript}); err != nil {
		t.Fatal(err)
	}

	return s, transcript
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	first, err := os.Stat(a)

	if err != nil {
		t.Fatal(err)
	}

	second, err := os.Stat(b)

	if err != nil {
		t.Fatal(err)
	}

	return os.SameFile(first, second)
}

func TestStarKeepsHardLinkAndPersists(t *testing.T) {
	s, transcript := starred(t)

	if !sameFile(t, transcript, s.CopyPath("abc")) {
		t.Fatal("copy is not a hard link of the transcript")
	}

	reopened, err := Open(s.Dir)

	if err != nil {
		t.Fatal(err)
	}

	if record := reopened.Find("abc"); record == nil || record.Name != "First" {
		t.Fatalf("record not persisted: %+v", record)
	}
}

func TestStarAgainOnlyUpdatesName(t *testing.T) {
	s, transcript := starred(t)
	starredAt := s.Find("abc").StarredAt

	if err := s.Star(Record{ID: "abc", Name: "Second", Transcript: transcript}); err != nil {
		t.Fatal(err)
	}

	if len(s.Records) != 1 || s.Records[0].Name != "Second" || !s.Records[0].StarredAt.Equal(starredAt) {
		t.Fatalf("unexpected records: %+v", s.Records)
	}
}

func TestSyncRestoresTranscriptDeletedByClaude(t *testing.T) {
	s, transcript := starred(t)
	old := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(transcript, old, old)
	os.Remove(transcript)

	if !s.OnlyCopy("abc") {
		t.Fatal("expected the copy to be the only one left")
	}

	s.Sync()

	if !sameFile(t, transcript, s.CopyPath("abc")) {
		t.Fatal("transcript was not restored from the copy")
	}

	info, _ := os.Stat(transcript)

	if time.Since(info.ModTime()) > time.Minute {
		t.Fatal("restored transcript keeps the old mtime and would be deleted again")
	}
}

func TestSyncRelinksRewrittenTranscript(t *testing.T) {
	s, transcript := starred(t)
	os.Remove(transcript)
	os.WriteFile(transcript, []byte(`{"type":"user","cwd":"/work"}`+"\n{}\n"), 0o600)

	s.Sync()

	if !sameFile(t, transcript, s.CopyPath("abc")) {
		t.Fatal("copy does not follow the rewritten transcript")
	}
}

func TestUnstarRemovesRecordAndCopy(t *testing.T) {
	s, transcript := starred(t)

	if err := s.Unstar("abc"); err != nil {
		t.Fatal(err)
	}

	if len(s.Records) != 0 {
		t.Fatal("record left after unstar")
	}

	if _, err := os.Stat(s.CopyPath("abc")); !os.IsNotExist(err) {
		t.Fatal("copy left after unstar")
	}

	if _, err := os.Stat(transcript); err != nil {
		t.Fatal("unstar must not touch the Claude transcript")
	}
}

func TestLostWhenBothFilesAreGone(t *testing.T) {
	s, transcript := starred(t)
	os.Remove(transcript)
	os.Remove(s.CopyPath("abc"))

	if !s.Lost("abc") {
		t.Fatal("expected the session to be lost")
	}
}

func TestChangesFromAnotherProcessAreNotLost(t *testing.T) {
	s, transcript := starred(t)
	other, _ := Open(s.Dir)
	second := filepath.Join(filepath.Dir(transcript), "def.jsonl")
	os.WriteFile(second, []byte("{}\n"), 0o600)

	if err := other.Star(Record{ID: "def", Name: "Other", Transcript: second}); err != nil {
		t.Fatal(err)
	}

	if err := s.Rename("abc", "Renamed"); err != nil {
		t.Fatal(err)
	}

	reopened, _ := Open(s.Dir)

	if len(reopened.Records) != 2 || reopened.Find("abc").Name != "Renamed" {
		t.Fatalf("a concurrent star was lost: %+v", reopened.Records)
	}
}

func TestStarAgainRefreshesLocation(t *testing.T) {
	s, _ := starred(t)
	moved := filepath.Join(t.TempDir(), "abc.jsonl")
	os.WriteFile(moved, []byte("{}\n"), 0o600)

	if err := s.Star(Record{ID: "abc", Name: "First", Cwd: "/moved", Project: "/moved", Transcript: moved}); err != nil {
		t.Fatal(err)
	}

	if record := s.Find("abc"); record.Transcript != moved || record.Cwd != "/moved" || record.Project != "/moved" {
		t.Fatalf("location not refreshed: %+v", record)
	}
}

func TestCorruptedListIsReported(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "starred.json"), []byte("{broken"), 0o600)

	if _, err := Open(dir); err == nil {
		t.Fatal("expected an error for a corrupted list")
	}
}
