package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/artengin/claude-starred/internal/store"
)

func newModel(t *testing.T) (*Model, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	s, _ := store.Open(filepath.Join(dir, "data"))
	now := time.Now()

	sessions := []struct {
		id, name, cwd, project string
		age                    time.Duration
	}{
		{"a", "Checkout flow", "/work/shop", "/work/shop", time.Hour},
		{"b", "Price migration", "/work/shop-2", "/work/shop", 2 * time.Hour},
		{"c", "Meeting notes", "/work/notes", "/work/notes", 30 * time.Minute},
		{"d", "Old notes", "/work/notes", "/work/notes", 3 * time.Hour},
	}

	for _, session := range sessions {
		transcript := filepath.Join(dir, "claude", "projects", "-x", session.id+".jsonl")
		must(t, os.MkdirAll(filepath.Dir(transcript), 0o700))
		must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
		modified := now.Add(-session.age)
		must(t, os.Chtimes(transcript, modified, modified))

		if err := s.Star(store.Record{ID: session.id, Name: session.name, Cwd: session.cwd, Project: session.project, Transcript: transcript}); err != nil {
			t.Fatal(err)
		}
	}

	model := New(s)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	return model, s
}

func press(m *Model, keys ...string) {
	for _, key := range keys {
		var msg tea.KeyMsg

		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}

		m.Update(msg)
	}
}

func assertContains(t *testing.T, view string, expected ...string) {
	t.Helper()

	for _, text := range expected {
		if !strings.Contains(view, text) {
			t.Errorf("view does not contain %q:\n%s", text, view)
		}
	}
}

func TestProjectsAreSortedByActivityAndShowNamesOrPaths(t *testing.T) {
	m, _ := newModel(t)
	view := m.View()
	assertContains(t, view, "Starred", "> notes", "shop")

	if strings.Index(view, "notes") > strings.Index(view, "shop") {
		t.Error("most recently active project should come first")
	}

	press(m, "p")
	assertContains(t, m.View(), "/work/notes", "/work/shop")
}

func TestSessionsShowWorktreeLabel(t *testing.T) {
	m, _ := newModel(t)
	press(m, "j", "enter")
	assertContains(t, m.View(), "Checkout flow", "Price migration  shop-2")
}

func TestSearchFiltersAndEscClears(t *testing.T) {
	m, _ := newModel(t)
	press(m, "/", "s", "h", "o")

	if rows := m.rows(); len(rows) != 1 || rows[0].text != "shop" {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	press(m, "enter", "esc")

	if len(m.rows()) != 2 {
		t.Fatal("esc should clear the search")
	}
}

func TestRenameAndUnstar(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "ctrl+u", "N", "e", "w", "enter")

	if s.Find("a").Name != "New" {
		t.Fatalf("rename failed: %+v", s.Find("a"))
	}

	press(m, "d", "n")

	if s.Find("a") == nil {
		t.Fatal("unstar must wait for confirmation")
	}

	press(m, "d", "y", "d", "y")

	if len(s.Records) != 2 || m.level != projectsLevel {
		t.Fatalf("expected the empty project to close: %+v", s.Records)
	}
}

func TestEnterOnSessionSelectsIt(t *testing.T) {
	m, _ := newModel(t)
	press(m, "enter", "enter")

	if m.Selected == nil || m.Selected.ID != "c" {
		t.Fatalf("unexpected selection: %+v", m.Selected)
	}
}

func TestEnterOnLiveSessionAsksForConfirmation(t *testing.T) {
	m, _ := newModel(t)
	sessions := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions")
	must(t, os.MkdirAll(sessions, 0o700))
	must(t, os.WriteFile(filepath.Join(sessions, "1.json"), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":"c"}`, os.Getpid())), 0o600))
	m.reload()
	press(m, "enter", "enter")

	if m.Selected != nil || m.mode != confirmingLive {
		t.Fatalf("a live session must ask before opening: selected %+v, mode %v", m.Selected, m.mode)
	}

	assertContains(t, m.View(), "open in another terminal")
	press(m, "n")

	if m.Selected != nil || m.mode != browsing {
		t.Fatal("n must cancel opening")
	}

	press(m, "enter", "y")

	if m.Selected == nil || m.Selected.ID != "c" {
		t.Fatalf("y must open the session: %+v", m.Selected)
	}
}

func TestRenameOutOfSearchKeepsCursorOnVisibleRow(t *testing.T) {
	m, _ := newModel(t)
	press(m, "j", "enter", "/", "c", "h", "e", "c", "k", "enter", "r", "ctrl+u", "P", "a", "y", "enter")

	if rows := m.rows(); len(rows) != 0 || m.cursor[sessionsLevel] != 0 {
		t.Fatalf("cursor must be clamped to the visible rows: %+v, cursor %d", rows, m.cursor[sessionsLevel])
	}

	press(m, "esc")

	if session := m.selectedSession(); session == nil || session.Name != "Pay" {
		t.Fatalf("expected the renamed session under the cursor: %+v", session)
	}
}

func TestProjectCursorFollowsTheProjectAfterUnstar(t *testing.T) {
	m, _ := newModel(t)
	press(m, "enter", "d", "y", "h")

	if projects := m.visibleProjects(); m.cursor[projectsLevel] != 1 || projects[1].root != "/work/notes" {
		t.Fatalf("cursor must stay on the reordered project: %d %+v", m.cursor[projectsLevel], projects)
	}
}

func TestRussianLayoutKeys(t *testing.T) {
	m, _ := newModel(t)
	press(m, "о")

	if m.cursor[projectsLevel] != 1 {
		t.Fatal("о should move down like j")
	}
}

func TestKeysArrivingTogetherAreHandledOneByOne(t *testing.T) {
	m, _ := newModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/sh")})

	if m.mode != searching || m.query[projectsLevel] != "sh" {
		t.Fatalf("mode %v, query %q", m.mode, m.query[projectsLevel])
	}
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
