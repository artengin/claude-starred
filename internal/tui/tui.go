package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/artengin/claude-starred/internal/claude"
	"github.com/artengin/claude-starred/internal/i18n"
	"github.com/artengin/claude-starred/internal/store"
)

type Session struct {
	store.Record
	Live    bool
	Lost    bool
	ModTime time.Time
}

type project struct {
	root     string
	sessions []Session
}

type level int

const (
	projectsLevel level = iota
	sessionsLevel
)

type mode int

const (
	browsing mode = iota
	searching
	renaming
	confirming
	confirmingLive
	helping
)

var (
	boldStyle = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
)

var russianLayout = strings.NewReplacer(
	"о", "j", "л", "k", "д", "l", "р", "h", "п", "g", "П", "G",
	"к", "r", "в", "d", "з", "p", "й", "q", "н", "y",
)

type Model struct {
	store     *store.Store
	projects  []project
	level     level
	root      string
	cursor    [2]int
	query     [2]string
	mode      mode
	input     string
	showPaths bool
	message   string
	width     int
	height    int
	Selected  *Session
}

func New(s *store.Store) *Model {
	model := &Model{store: s}
	s.Sync()
	model.reload()

	return model
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) tea.Cmd {
	if key.Type == tea.KeyCtrlC {
		return tea.Quit
	}

	if key.Type == tea.KeyRunes && len(key.Runes) > 1 && m.mode != searching && m.mode != renaming {
		var commands []tea.Cmd

		for _, r := range key.Runes {
			commands = append(commands, m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}

		return tea.Batch(commands...)
	}

	switch m.mode {
	case searching:
		m.editInput(key, m.finishSearch, func() { m.query[m.level] = m.input; m.cursor[m.level] = 0 })
	case renaming:
		m.editInput(key, m.finishRename, nil)
	case confirming:
		m.finishUnstar(russianLayout.Replace(key.String()) == "y")
	case confirmingLive:
		return m.finishOpen(russianLayout.Replace(key.String()) == "y")
	case helping:
		m.mode = browsing
	default:
		return m.browse(key)
	}

	return nil
}

func (m *Model) browse(key tea.KeyMsg) tea.Cmd {
	m.message = ""

	switch russianLayout.Replace(key.String()) {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.cursor[m.level] = 0
	case "G", "end":
		m.cursor[m.level] = len(m.rows()) - 1
	case "l", "right", "enter":
		return m.open()
	case "h", "left", "esc":
		m.back()
	case "/":
		m.mode, m.input = searching, m.query[m.level]
	case "r":
		if session := m.selectedSession(); session != nil {
			m.mode, m.input = renaming, session.Name
		}
	case "d":
		if m.selectedSession() != nil {
			m.mode = confirming
		}
	case "p":
		m.showPaths = !m.showPaths
	case "?":
		m.mode = helping
	case "q":
		return tea.Quit
	}

	m.clampCursor()

	return nil
}

func (m *Model) editInput(key tea.KeyMsg, done func(), changed func()) {
	switch key.Type {
	case tea.KeyEnter:
		done()
		m.mode = browsing
		return
	case tea.KeyEsc:
		if m.mode == searching {
			m.query[m.level] = ""
		}

		m.mode = browsing
		return
	case tea.KeyBackspace:
		runes := []rune(m.input)

		if len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		m.input = ""
	case tea.KeyRunes, tea.KeySpace:
		m.input += string(key.Runes)
	default:
		return
	}

	if changed != nil {
		changed()
	}
}

func (m *Model) finishSearch() {
	m.query[m.level] = m.input
}

func (m *Model) finishRename() {
	session := m.selectedSession()
	name := strings.TrimSpace(m.input)

	if session == nil || name == "" || name == session.Name {
		return
	}

	if err := m.store.Rename(session.ID, name); err != nil {
		m.message = err.Error()
		return
	}

	m.reload()
	m.message = i18n.T("renamed", name)
}

func (m *Model) finishUnstar(confirmed bool) {
	m.mode = browsing
	session := m.selectedSession()

	if !confirmed || session == nil {
		return
	}

	if err := m.store.Unstar(session.ID); err != nil {
		m.message = err.Error()
		return
	}

	m.reload()
	m.message = i18n.T("unstarred", session.Name)

	if m.currentProject() == nil {
		m.level = projectsLevel
	}
}

func (m *Model) open() tea.Cmd {
	if m.level == projectsLevel {
		selected := m.selectedProject()

		if selected == nil {
			return nil
		}

		m.root = selected.root
		m.level, m.cursor[sessionsLevel], m.query[sessionsLevel] = sessionsLevel, 0, ""

		return nil
	}

	session := m.selectedSession()

	if session == nil {
		return nil
	}

	if session.Lost {
		m.message = i18n.T("lost_session")
		return nil
	}

	if claude.LiveSessionIDs()[session.ID] {
		m.mode = confirmingLive
		return nil
	}

	m.Selected = session

	return tea.Quit
}

func (m *Model) finishOpen(confirmed bool) tea.Cmd {
	m.mode = browsing
	session := m.selectedSession()

	if !confirmed || session == nil {
		return nil
	}

	m.Selected = session

	return tea.Quit
}

func (m *Model) back() {
	if m.query[m.level] != "" {
		m.query[m.level] = ""
	} else if m.level == sessionsLevel {
		m.level = projectsLevel
	}
}

func (m *Model) move(delta int) {
	m.cursor[m.level] += delta
}

func (m *Model) clampCursor() {
	m.cursor[projectsLevel] = max(0, min(m.cursor[projectsLevel], len(m.visibleProjects())-1))
	m.cursor[sessionsLevel] = max(0, min(m.cursor[sessionsLevel], len(m.visibleSessions())-1))
}

func (m *Model) selection() (root, id string) {
	if m.level == sessionsLevel {
		root = m.root
	} else if selected := m.selectedProject(); selected != nil {
		root = selected.root
	}

	if session := m.selectedSession(); session != nil {
		id = session.ID
	}

	return root, id
}

func (m *Model) restoreSelection(root, id string) {
	for i, p := range m.visibleProjects() {
		if p.root == root {
			m.cursor[projectsLevel] = i
		}
	}

	for i, session := range m.visibleSessions() {
		if session.ID == id {
			m.cursor[sessionsLevel] = i
		}
	}

	m.clampCursor()
}

func (m *Model) reload() {
	root, id := m.selection()

	if err := m.store.Reload(); err != nil {
		m.message = err.Error()
	}

	live := claude.LiveSessionIDs()
	indexByRoot := map[string]int{}
	m.projects = nil

	for _, record := range m.store.Records {
		index, ok := indexByRoot[record.Project]

		if !ok {
			index = len(m.projects)
			indexByRoot[record.Project] = index
			m.projects = append(m.projects, project{root: record.Project})
		}

		session := Session{Record: record, Live: live[record.ID], Lost: m.store.Lost(record.ID), ModTime: modTime(record, m.store)}
		m.projects[index].sessions = append(m.projects[index].sessions, session)
	}

	for _, p := range m.projects {
		sort.SliceStable(p.sessions, func(a, b int) bool { return p.sessions[a].ModTime.After(p.sessions[b].ModTime) })
	}

	sort.SliceStable(m.projects, func(a, b int) bool {
		return m.projects[a].sessions[0].ModTime.After(m.projects[b].sessions[0].ModTime)
	})

	m.restoreSelection(root, id)
}

func modTime(record store.Record, s *store.Store) time.Time {
	for _, path := range []string{record.Transcript, s.CopyPath(record.ID)} {
		if info, err := os.Stat(path); err == nil {
			return info.ModTime()
		}
	}

	return record.StarredAt
}

func (m *Model) currentProject() *project {
	for i := range m.projects {
		if m.projects[i].root == m.root {
			return &m.projects[i]
		}
	}

	return nil
}

func (m *Model) visibleProjects() []project {
	var visible []project

	for _, p := range m.projects {
		if matches(m.projectLabel(p.root), m.query[projectsLevel]) {
			visible = append(visible, p)
		}
	}

	return visible
}

func (m *Model) visibleSessions() []Session {
	current := m.currentProject()

	if current == nil {
		return nil
	}

	var visible []Session

	for _, session := range current.sessions {
		if matches(session.Name, m.query[sessionsLevel]) {
			visible = append(visible, session)
		}
	}

	return visible
}

func (m *Model) selectedProject() *project {
	projects := m.visibleProjects()

	if len(projects) == 0 {
		return nil
	}

	return &projects[max(0, min(m.cursor[projectsLevel], len(projects)-1))]
}

func (m *Model) selectedSession() *Session {
	if m.level != sessionsLevel {
		return nil
	}

	sessions := m.visibleSessions()

	if len(sessions) == 0 {
		return nil
	}

	return &sessions[max(0, min(m.cursor[sessionsLevel], len(sessions)-1))]
}

type row struct {
	text    string
	detail  string
	keepEnd bool
}

func (m *Model) rows() []row {
	var rows []row

	if m.level == projectsLevel {
		for _, p := range m.visibleProjects() {
			rows = append(rows, row{text: m.projectLabel(p.root), keepEnd: m.showPaths})
		}

		return rows
	}

	for _, session := range m.visibleSessions() {
		rows = append(rows, row{text: m.sessionLabel(session), detail: m.sessionDetail(session)})
	}

	return rows
}

func (m *Model) projectLabel(root string) string {
	if m.showPaths {
		return shortenHome(root)
	}

	return filepath.Base(root)
}

func (m *Model) sessionLabel(session Session) string {
	marker := "  "

	if session.Live {
		marker = "● "
	}

	return marker + session.Name
}

func (m *Model) sessionDetail(session Session) string {
	if session.Lost {
		return i18n.T("lost")
	}

	if m.showPaths {
		return shortenHome(session.Cwd)
	}

	if session.Cwd != session.Project && !strings.HasPrefix(session.Cwd, session.Project+string(filepath.Separator)) {
		return filepath.Base(session.Cwd)
	}

	return ""
}

func (m *Model) View() string {
	if m.mode == helping {
		return m.helpView()
	}

	title := i18n.T("title")

	if m.level == sessionsLevel {
		title = m.projectLabel(m.root)
	}

	if query := m.query[m.level]; query != "" && m.mode != searching {
		title += "  /" + query
	}

	lines := []string{
		" " + boldStyle.Render("★ "+fitStart(title, m.lineWidth()-4)),
		m.rule(),
		"",
	}
	rows := m.rows()
	visible := max(1, m.height-6)
	top := max(0, m.cursor[m.level]-visible+1)

	if len(m.store.Records) == 0 {
		lines = append(lines, "  "+dimStyle.Render(i18n.T("empty")))
	} else if len(rows) == 0 {
		lines = append(lines, "  "+dimStyle.Render(i18n.T("not_found")))
	}

	for i := top; i < len(rows) && i < top+visible; i++ {
		lines = append(lines, m.renderRow(rows[i], i == m.cursor[m.level]))
	}

	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n") + "\n" + m.footer()
}

func (m *Model) rule() string {
	width := m.width - 2

	if m.width <= 0 {
		width = 40
	}

	return " " + dimStyle.Render(strings.Repeat("─", max(width, 1)))
}

func (m *Model) renderRow(r row, selected bool) string {
	prefix := "   "

	if selected {
		prefix = " > "
	}

	available := m.lineWidth() - runewidth.StringWidth(prefix) - 1

	detail := ""

	if r.detail != "" {
		detailWidth := min(runewidth.StringWidth(r.detail), available/2)
		detail = "  " + dimStyle.Render(fitStart(r.detail, detailWidth))
		available -= detailWidth + 2
	}

	text := fitEnd(r.text, available)

	if r.keepEnd {
		text = fitStart(r.text, available)
	}

	if selected {
		text = boldStyle.Render(text)
	}

	return prefix + text + detail
}

func (m *Model) footer() string {
	text, style := m.footerText()

	return " " + style.Render(fitEnd(text, m.lineWidth()-2))
}

func (m *Model) footerText() (string, lipgloss.Style) {
	plain := lipgloss.NewStyle()

	switch m.mode {
	case searching:
		return "/" + m.input + "█", plain
	case renaming:
		return i18n.T("prompt_rename") + m.input + "█", plain
	case confirming, confirmingLive:
		if session := m.selectedSession(); session != nil {
			return m.confirmation(*session), plain
		}
	}

	if m.message != "" {
		return m.message, plain
	}

	if m.level == projectsLevel {
		return i18n.T("hint_projects"), dimStyle
	}

	return i18n.T("hint_sessions"), dimStyle
}

func (m *Model) confirmation(session Session) string {
	switch {
	case m.mode == confirmingLive:
		return i18n.T("confirm_live", session.Name)
	case m.store.OnlyCopy(session.ID):
		return i18n.T("confirm_last", session.Name)
	}

	return i18n.T("confirm_unstar", session.Name)
}

func (m *Model) lineWidth() int {
	if m.width <= 0 {
		return 1 << 20
	}

	return m.width
}

func (m *Model) helpView() string {
	keys := [][2]string{
		{"j / k, ↓ / ↑", "help_move"},
		{"l, enter, →", "help_open"},
		{"h, esc, ←", "help_back"},
		{"/", "help_search"},
		{"g / G", "help_edges"},
		{"r", "help_rename"},
		{"d", "help_unstar"},
		{"p", "help_paths"},
		{"q", "help_quit"},
	}
	lines := []string{" " + boldStyle.Render(i18n.T("help")), ""}

	for _, key := range keys {
		lines = append(lines, "   "+lipgloss.NewStyle().Width(16).Render(key[0])+i18n.T(key[1]))
	}

	return strings.Join(append(lines, "", " "+dimStyle.Render(i18n.T("help_close"))), "\n")
}

func matches(text, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(text), strings.ToLower(query))
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()

	if err == nil && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + path[len(home):]
	}

	return path
}

func fitEnd(text string, width int) string {
	return runewidth.Truncate(text, max(width, 1), "…")
}

func fitStart(text string, width int) string {
	width = max(width, 1)

	if runewidth.StringWidth(text) <= width {
		return text
	}

	runes := []rune(text)

	for i := range runes {
		if runewidth.StringWidth(string(runes[i:])) <= width-1 {
			return "…" + string(runes[i:])
		}
	}

	return "…"
}
