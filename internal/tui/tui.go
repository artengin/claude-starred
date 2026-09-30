package tui

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/artengin/claude-starred/internal/claude"
	"github.com/artengin/claude-starred/internal/i18n"
	"github.com/artengin/claude-starred/internal/store"
)

type Session struct {
	store.Record
	Live bool
	Lost bool
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
	renaming
	confirming
	confirmingLive
	helping
)

var (
	boldStyle = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
	collator  = collate.New(language.Und, collate.IgnoreCase, collate.Numeric)
)

type hint struct {
	key   string
	label string
}

const (
	hintSeparator   = " · "
	refreshInterval = 2 * time.Second
)

type tickMsg struct{}

var (
	projectHints = []hint{{"enter", "help_open"}, {"p", "hint_paths"}, {"?", "hint_help"}, {"q", "help_quit"}}
	sessionHints = []hint{{"enter", "help_open"}, {"r", "hint_rename"}, {"d", "hint_unstar"}, {"p", "hint_paths"}, {"h", "help_back"}, {"?", "hint_help"}}
	helpKeys     = []hint{
		{"j / k, ↓ / ↑", "help_move"},
		{"l, enter, →", "help_open"},
		{"h, esc, ←", "help_back"},
		{"g / G", "help_edges"},
		{"r", "help_rename"},
		{"d", "help_unstar"},
		{"p", "help_paths"},
		{"?", "hint_help"},
		{"q", "help_quit"},
	}
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
	mode      mode
	input     []rune
	caret     int
	showPaths bool
	message   string
	failure   string
	width     int
	height    int
	Selected  *Session
}

func New(s *store.Store) *Model {
	model := &Model{store: s}

	if err := s.Sync(); err != nil {
		model.message = i18n.T("sync_failed", strings.ReplaceAll(err.Error(), "\n", "; "))
	}

	model.reload()

	return model
}

func (m *Model) Init() tea.Cmd {
	return tick()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case tickMsg:
		if m.mode == browsing {
			m.reload()
		}

		return m, tick()
	}

	return m, nil
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) handleKey(key tea.KeyMsg) tea.Cmd {
	if key.Type == tea.KeyCtrlC {
		return tea.Quit
	}

	if key.Paste && m.mode != renaming {
		return nil
	}

	if key.Type == tea.KeyRunes && len(key.Runes) > 1 && m.mode != renaming {
		var commands []tea.Cmd

		for _, r := range key.Runes {
			commands = append(commands, m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}

		return tea.Batch(commands...)
	}

	switch m.mode {
	case renaming:
		m.editInput(key, m.finishRename)
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
	case "r":
		if session := m.selectedSession(); session != nil {
			m.startRename(*session)
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

func (m *Model) startRename(session Session) {
	m.mode = renaming
	m.input = []rune(session.Name)
	m.caret = len(m.input)
}

func (m *Model) editInput(key tea.KeyMsg, done func()) {
	switch key.Type {
	case tea.KeyEnter:
		done()
		m.mode = browsing
	case tea.KeyEsc:
		m.mode = browsing
	case tea.KeyLeft:
		m.caret = max(m.caret-1, 0)
	case tea.KeyRight:
		m.caret = min(m.caret+1, len(m.input))
	case tea.KeyHome, tea.KeyCtrlA:
		m.caret = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		m.caret = len(m.input)
	case tea.KeyBackspace:
		if m.caret > 0 {
			m.input = slices.Delete(m.input, m.caret-1, m.caret)
			m.caret--
		}
	case tea.KeyDelete:
		if m.caret < len(m.input) {
			m.input = slices.Delete(m.input, m.caret, m.caret+1)
		}
	case tea.KeyCtrlU:
		m.input, m.caret = nil, 0
	case tea.KeyRunes, tea.KeySpace:
		m.input = slices.Insert(m.input, m.caret, key.Runes...)
		m.caret += len(key.Runes)
	}
}

func (m *Model) inputView(width int) string {
	width = max(width-1, 0)
	head, tail := string(m.input[:m.caret]), string(m.input[m.caret:])
	tailWidth := min(runewidth.StringWidth(tail), max(width-runewidth.StringWidth(head), width/3, visibleNext(m.input[m.caret:])))

	if headWidth := width - tailWidth; headWidth > 0 {
		head = fitStart(head, headWidth)
	} else {
		head = ""
	}

	if tailWidth < runewidth.StringWidth(tail) {
		tail = fitEnd(tail, tailWidth)
	}

	return head + "█" + tail
}

func visibleNext(tail []rune) int {
	if len(tail) == 0 {
		return 0
	}

	if len(tail) == 1 {
		return runewidth.RuneWidth(tail[0])
	}

	return runewidth.RuneWidth(tail[0]) + 1
}

func (m *Model) finishRename() {
	session := m.selectedSession()
	name := strings.TrimSpace(string(m.input))

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
}

func (m *Model) open() tea.Cmd {
	if m.level == projectsLevel {
		selected := m.selectedProject()

		if selected == nil {
			return nil
		}

		m.root = selected.root
		m.level, m.cursor[sessionsLevel] = sessionsLevel, 0

		return nil
	}

	session := m.selectedSession()

	if session == nil {
		return nil
	}

	if err := m.store.SyncRecord(session.Record); err != nil {
		m.message = i18n.T("sync_failed", err.Error())
		return nil
	}

	if m.store.Lost(session.ID) {
		m.message = i18n.T("lost_session")
		return nil
	}

	if session.Live {
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
	if m.level == sessionsLevel {
		m.level = projectsLevel
	}
}

func (m *Model) move(delta int) {
	m.cursor[m.level] += delta
}

func (m *Model) clampCursor() {
	m.cursor[projectsLevel] = max(0, min(m.cursor[projectsLevel], len(m.projects)-1))
	m.cursor[sessionsLevel] = max(0, min(m.cursor[sessionsLevel], len(m.sessions())-1))
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
	for i, p := range m.projects {
		if p.root == root {
			m.cursor[projectsLevel] = i
		}
	}

	for i, session := range m.sessions() {
		if session.ID == id {
			m.cursor[sessionsLevel] = i
		}
	}

	m.clampCursor()
}

func (m *Model) reload() {
	root, id := m.selection()
	m.failure = ""

	if err := m.store.Reload(); err != nil {
		m.failure = err.Error()
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

		session := Session{Record: record, Live: live[record.ID], Lost: m.store.Lost(record.ID)}
		m.projects[index].sessions = append(m.projects[index].sessions, session)
	}

	for _, p := range m.projects {
		sort.SliceStable(p.sessions, func(a, b int) bool { return before(p.sessions[a].Name, p.sessions[b].Name) })
	}

	sort.SliceStable(m.projects, func(a, b int) bool {
		first, second := filepath.Base(m.projects[a].root), filepath.Base(m.projects[b].root)

		if first == second {
			return m.projects[a].root < m.projects[b].root
		}

		return before(first, second)
	})

	if m.level == sessionsLevel && m.currentProject() == nil {
		m.level = projectsLevel
	}

	m.restoreSelection(root, id)
}

func before(a, b string) bool {
	return collator.CompareString(a, b) < 0
}

func (m *Model) currentProject() *project {
	for i := range m.projects {
		if m.projects[i].root == m.root {
			return &m.projects[i]
		}
	}

	return nil
}

func (m *Model) sessions() []Session {
	current := m.currentProject()

	if current == nil {
		return nil
	}

	return current.sessions
}

func (m *Model) selectedProject() *project {
	if len(m.projects) == 0 {
		return nil
	}

	return &m.projects[max(0, min(m.cursor[projectsLevel], len(m.projects)-1))]
}

func (m *Model) selectedSession() *Session {
	if m.level != sessionsLevel {
		return nil
	}

	sessions := m.sessions()

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
		for _, p := range m.projects {
			rows = append(rows, row{text: m.projectLabel(p.root), keepEnd: m.showPaths})
		}

		return rows
	}

	for _, session := range m.sessions() {
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
	marker := "○ "

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
	width := m.lineWidth() - 2

	if m.mode == renaming {
		prompt := i18n.T("prompt_rename")

		if runewidth.StringWidth(prompt)+4 > width {
			prompt = ""
		}

		return " " + prompt + m.inputView(width-runewidth.StringWidth(prompt))
	}

	if text := m.footerText(); text != "" {
		return " " + fitEnd(text, width)
	}

	return " " + m.hints(width)
}

func (m *Model) footerText() string {
	switch m.mode {
	case confirming, confirmingLive:
		if session := m.selectedSession(); session != nil {
			return m.confirmation(*session)
		}
	}

	if m.message != "" {
		return m.message
	}

	return m.failure
}

func (m *Model) hints(width int) string {
	hints := sessionHints

	if m.level == projectsLevel {
		hints = projectHints
	}

	var parts []string
	used := 0

	for i, h := range hints {
		description := i18n.T(h.label)
		size := runewidth.StringWidth(h.key) + 1 + runewidth.StringWidth(description)

		if i > 0 {
			size += runewidth.StringWidth(hintSeparator)
		}

		if used+size > width {
			if i == 0 {
				parts = append(parts, dimStyle.Render(fitEnd(h.key+" "+description, width)))
			}

			break
		}

		used += size
		parts = append(parts, boldStyle.Render(h.key)+" "+dimStyle.Render(description))
	}

	return strings.Join(parts, dimStyle.Render(hintSeparator))
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
	lines := []string{" " + boldStyle.Render(i18n.T("help")), ""}

	for _, h := range helpKeys {
		lines = append(lines, "   "+lipgloss.NewStyle().Width(16).Render(h.key)+i18n.T(h.label))
	}

	return strings.Join(append(lines, "", " "+dimStyle.Render(i18n.T("help_close"))), "\n")
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
