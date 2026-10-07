package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/artengin/claude-starred/internal/store"
)

func newModel(t *testing.T) (*Model, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	lipgloss.SetColorProfile(termenv.Ascii)
	s, _ := store.Open(filepath.Join(dir, "data"))

	sessions := []struct {
		id, name, cwd, project string
	}{
		{"a", "Checkout flow", "/work/shop", "/work/shop"},
		{"b", "Price migration", "/work/shop-2", "/work/shop"},
		{"c", "Meeting notes", "/work/notes", "/work/notes"},
		{"d", "Old notes", "/work/notes", "/work/notes"},
	}

	for _, session := range sessions {
		transcript := filepath.Join(dir, "claude", "projects", "-x", session.id+".jsonl")
		must(t, os.MkdirAll(filepath.Dir(transcript), 0o700))
		must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))

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
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "home":
			msg = tea.KeyMsg{Type: tea.KeyHome}
		case "end":
			msg = tea.KeyMsg{Type: tea.KeyEnd}
		case "ctrl+a":
			msg = tea.KeyMsg{Type: tea.KeyCtrlA}
		case "ctrl+e":
			msg = tea.KeyMsg{Type: tea.KeyCtrlE}
		case "delete":
			msg = tea.KeyMsg{Type: tea.KeyDelete}
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

func TestProjectsAreSortedByNameAndShowNamesOrPaths(t *testing.T) {
	m, _ := newModel(t)
	view := m.View()
	assertContains(t, view, "Starred", "> notes", "shop")

	if strings.Index(view, "notes") > strings.Index(view, "shop") {
		t.Error("projects should be sorted by name")
	}

	press(m, "p")
	assertContains(t, m.View(), "/work/notes", "/work/shop")
}

func TestSessionsShowWorktreeLabel(t *testing.T) {
	m, _ := newModel(t)
	press(m, "j", "enter")
	assertContains(t, m.View(), "Checkout flow", "Price migration  shop-2")
}

func TestSessionsAreMarkedOnlineOrOffline(t *testing.T) {
	m, _ := newModel(t)
	sessions := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions")
	must(t, os.MkdirAll(sessions, 0o700))
	must(t, os.WriteFile(filepath.Join(sessions, "1.json"), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":"c"}`, os.Getpid())), 0o600))
	m.reload()
	press(m, "enter")
	assertContains(t, m.View(), "● Meeting notes", "○ Old notes")
}

func TestEscGoesBack(t *testing.T) {
	m, _ := newModel(t)
	press(m, "enter", "esc")

	if m.level != projectsLevel {
		t.Fatal("esc should go back to projects")
	}
}

func TestUnstarOffersToDropTheCopyOnlyWhenItCannotGoBack(t *testing.T) {
	m, s := newModel(t)
	transcript := s.Find("c").Transcript
	must(t, os.Remove(transcript))
	press(m, "enter", "d")

	if !strings.Contains(m.View(), "goes back to Claude") {
		t.Fatalf("expected the return notice, got:\n%s", m.View())
	}

	press(m, "y")

	if s.Find("c") != nil || !exists(transcript) {
		t.Fatal("unstar must return the transcript to Claude")
	}

	must(t, os.Remove(s.Find("d").Transcript))
	must(t, os.RemoveAll(filepath.Dir(transcript)))
	must(t, os.WriteFile(filepath.Dir(transcript), nil, 0o600))
	press(m, "d", "y", "y")

	if s.Find("d") == nil || !exists(s.CopyPath("d")) || m.mode != browsing {
		t.Fatal("a repeated y must keep the last copy and close the question")
	}

	press(m, "d", "y")

	if m.mode != confirmingLast || m.returnError == nil || !strings.Contains(m.View(), "Last copy of «Old notes»: press D") {
		t.Fatalf("the question must survive the footer width, got:\n%s", m.View())
	}

	press(m, "n")

	if s.Find("d") == nil || !exists(s.CopyPath("d")) {
		t.Fatal("declining must keep the record and the copy")
	}

	press(m, "d", "y", "D")

	if s.Find("d") != nil || exists(s.CopyPath("d")) {
		t.Fatal("confirmed loss must discard the record and the copy")
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

func TestCursorFollowsTheRenamedSessionWhenItMoves(t *testing.T) {
	m, _ := newModel(t)
	press(m, "enter", "r", "ctrl+u", "Z", "z", "z", "enter")

	if session := m.selectedSession(); m.cursor[sessionsLevel] != 1 || session == nil || session.Name != "Zzz" {
		t.Fatalf("cursor must follow the renamed session: %d %+v", m.cursor[sessionsLevel], session)
	}
}

func TestTickReloadsTheListWhileBrowsing(t *testing.T) {
	m, s := newModel(t)
	transcript := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-x", "e.jsonl")
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	other, err := store.Open(s.Dir)
	must(t, err)
	must(t, other.Star(store.Record{ID: "e", Name: "Alpha", Cwd: "/work/api", Project: "/work/api", Transcript: transcript}))

	press(m, "enter", "r")
	m.Update(tickMsg{})

	if len(m.projects) != 2 {
		t.Fatal("tick must not reload while a name is being edited")
	}

	press(m, "esc", "h")
	m.Update(tickMsg{})

	if len(m.projects) != 3 || m.projects[0].root != "/work/api" {
		t.Fatalf("tick must pick up a session starred elsewhere: %+v", m.projects)
	}
}

func TestTickKeepsCursorAndUpdatesMarkers(t *testing.T) {
	m, s := newModel(t)
	press(m, "enter", "j")
	other, err := store.Open(s.Dir)
	must(t, err)
	must(t, other.Rename("c", "Zzz"))
	sessions := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions")
	must(t, os.MkdirAll(sessions, 0o700))
	must(t, os.WriteFile(filepath.Join(sessions, "1.json"), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":"d"}`, os.Getpid())), 0o600))

	m.Update(tickMsg{})

	if session := m.selectedSession(); m.cursor[sessionsLevel] != 0 || session == nil || session.ID != "d" {
		t.Fatalf("cursor must stay on the same session after a reorder: %d %+v", m.cursor[sessionsLevel], session)
	}

	assertContains(t, m.View(), "● Old notes", "○ Zzz")
}

func TestTickLeavesAProjectThatDisappeared(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter")
	other, err := store.Open(s.Dir)
	must(t, err)
	must(t, other.Unstar("a"))
	must(t, other.Unstar("b"))

	m.Update(tickMsg{})

	if m.level != projectsLevel {
		t.Fatal("tick must go back to projects when the open project is gone")
	}
}

func TestProjectsWithTheSameNameAreOrderedByPath(t *testing.T) {
	m, s := newModel(t)
	transcript := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-x", "e.jsonl")
	must(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	must(t, s.Star(store.Record{ID: "e", Name: "Other", Cwd: "/home/shop", Project: "/home/shop", Transcript: transcript}))
	m.reload()

	roots := []string{m.projects[0].root, m.projects[1].root, m.projects[2].root}

	if expected := []string{"/work/notes", "/home/shop", "/work/shop"}; !slices.Equal(roots, expected) {
		t.Fatalf("unexpected order: %v", roots)
	}
}

func TestNamesAreSortedNaturally(t *testing.T) {
	names := []string{"Session 10", "Ёлка", "Яблоко", "session 2", "apple"}
	sort.SliceStable(names, func(a, b int) bool { return before(names[a], names[b]) })

	if expected := []string{"apple", "session 2", "Session 10", "Ёлка", "Яблоко"}; !slices.Equal(names, expected) {
		t.Fatalf("unexpected order: %v", names)
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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jl")})

	if m.level != sessionsLevel || m.root != "/work/shop" {
		t.Fatalf("level %v, root %q", m.level, m.root)
	}
}

func TestPastedTextIsNotTreatedAsKeys(t *testing.T) {
	m, s := newModel(t)
	press(m, "enter")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dy"), Paste: true})

	if len(s.Records) != 4 || m.mode != browsing {
		t.Fatalf("pasted text must be ignored: %d records, mode %v", len(s.Records), m.mode)
	}
}

func TestOpenRestoresATranscriptDeletedAfterStart(t *testing.T) {
	m, s := newModel(t)
	transcript := s.Find("c").Transcript
	must(t, os.Remove(transcript))
	press(m, "enter", "enter")

	if m.Selected == nil || m.Selected.ID != "c" || !exists(transcript) {
		t.Fatalf("open must bring the transcript back before resuming: %+v", m.Selected)
	}
}

func TestRenameAcceptsRunesArrivingTogether(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "ctrl+u")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Pay")})
	press(m, "enter")

	if s.Find("a").Name != "Pay" {
		t.Fatalf("rename failed: %+v", s.Find("a"))
	}

	if session := m.selectedSession(); session == nil || session.Name != "Pay" {
		t.Fatalf("expected the renamed session under the cursor: %+v", session)
	}
}

func TestRenameInsertsAtCursor(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "left", "left", "X", "enter")

	if s.Find("a").Name != "Checkout flXow" {
		t.Fatalf("rename failed: %+v", s.Find("a"))
	}
}

func TestRenameEditsAroundCursor(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "home", "delete", "c", "right", "backspace", "H", "end", "backspace", "W")

	if s.Find("a").Name != "Checkout flow" {
		t.Fatalf("unexpected name before enter: %+v", s.Find("a"))
	}

	press(m, "enter")

	if s.Find("a").Name != "cHeckout floW" {
		t.Fatalf("rename failed: %+v", s.Find("a"))
	}
}

func TestRenameShowsCursorInFooter(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "home", "right")

	if footer := m.footer(); footer != " Name: C█heckout flow" {
		t.Fatalf("unexpected footer %q", footer)
	}

	press(m, "esc")

	if s.Find("a").Name != "Checkout flow" || m.mode != browsing {
		t.Fatal("esc must cancel the rename")
	}
}

func TestCtrlUClearsTheWholeName(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "left", "left", "ctrl+u", "N", "e", "w", "enter")

	if s.Find("a").Name != "New" {
		t.Fatalf("ctrl+u must clear the whole name: %+v", s.Find("a"))
	}
}

func TestRenameFooterFitsTheWindowAndKeepsTheCursorVisible(t *testing.T) {
	m, _ := newModel(t)
	press(m, "j", "enter", "r", "home", "right", "right")

	for _, width := range []int{5, 8, 12, 16, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		footer := m.footer()

		if runewidth.StringWidth(footer) > width-1 {
			t.Errorf("width %d: footer %q is %d cells wide", width, footer, runewidth.StringWidth(footer))
		}

		if !strings.Contains(footer, "█e") {
			t.Errorf("width %d: the cursor or the character under it is hidden in %q", width, footer)
		}
	}
}

func TestWideCharacterUnderTheCursorStaysVisible(t *testing.T) {
	m, _ := newModel(t)
	press(m, "j", "enter", "r", "ctrl+u")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("日本語のセッション名")})
	press(m, "home", "right")
	m.Update(tea.WindowSizeMsg{Width: 12, Height: 20})

	if footer := m.footer(); !strings.Contains(footer, "█本") {
		t.Fatalf("the wide character under the cursor is hidden in %q", footer)
	}
}

func TestRenameCursorStopsAtTheEdges(t *testing.T) {
	m, s := newModel(t)
	press(m, "j", "enter", "r", "ctrl+a", "backspace", "left", "X", "ctrl+e", "delete", "right", "Y", "enter")

	if s.Find("a").Name != "XCheckout flowY" {
		t.Fatalf("unexpected name %q", s.Find("a").Name)
	}
}

func TestFooterDropsHintsThatDoNotFit(t *testing.T) {
	m, _ := newModel(t)
	press(m, "enter")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	footer := m.footer()

	if footer != " enter open · r rename · d unstar" {
		t.Fatalf("unexpected footer %q", footer)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
