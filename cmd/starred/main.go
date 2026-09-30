package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/artengin/claude-starred/internal/claude"
	"github.com/artengin/claude-starred/internal/i18n"
	"github.com/artengin/claude-starred/internal/setup"
	"github.com/artengin/claude-starred/internal/store"
	"github.com/artengin/claude-starred/internal/tui"
)

var version = "dev"

const usage = `starred - starred Claude Code sessions

Usage:
  starred                      browse starred sessions
  starred star <id> [--name N] star a session (used by the /star skill)
  starred info <id>            show the star state of a session
  starred install              install the /star skill into Claude Code
  starred uninstall            remove the skill, starred data and this binary
  starred update               update to the latest release
  starred version              print the version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "starred:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := ""

	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "version":
		fmt.Println(version)
		return nil
	case "install":
		return install()
	case "update":
		return update()
	case "uninstall":
		return uninstall()
	case "", "star", "info":
	default:
		fmt.Print(usage)
		return nil
	}

	s, err := store.Open(store.DefaultDir())

	if err != nil {
		return err
	}

	switch command {
	case "star":
		return star(s, args[1:])
	case "info":
		return info(s, args[1:])
	}

	return browse(s)
}

func browse(s *store.Store) error {
	model := tui.New(s)

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return err
	}

	if model.Selected == nil {
		return nil
	}

	return resume(*model.Selected)
}

func resume(session tui.Session) error {
	home, _ := os.UserHomeDir()
	directory := home

	for _, candidate := range []string{session.Cwd, session.Project} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			directory = candidate
			break
		}
	}

	if directory != session.Cwd {
		fmt.Println(i18n.T("moved_from", session.Cwd, directory))
	}

	if err := os.Chdir(directory); err != nil {
		return err
	}

	return execClaude("--resume", session.ID)
}

func star(s *store.Store, args []string) error {
	flags := flag.NewFlagSet("star", flag.ContinueOnError)
	name := flags.String("name", "", "session name, or - to read it from stdin")

	if len(args) == 0 {
		return errors.New("usage: starred star <id> [--name N]")
	}

	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	id := args[0]

	if *name == "-" {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		*name = strings.TrimSpace(line)
	}

	transcriptPath, err := claude.FindTranscript(id)

	if errors.Is(err, os.ErrNotExist) {
		return errors.New(i18n.T("no_transcript", id))
	}

	if err != nil {
		return err
	}

	transcript, err := claude.ReadTranscript(transcriptPath)

	if err != nil {
		return err
	}

	if transcript.Cwd == "" {
		return errors.New(i18n.T("no_transcript", id))
	}

	record := store.Record{
		ID:         id,
		Name:       firstNonEmpty(*name, currentName(s, id, transcript), transcript.AITitle, id),
		Cwd:        transcript.Cwd,
		Project:    claude.ProjectRoot(transcript.Cwd),
		Transcript: transcriptPath,
	}

	if err := s.Star(record); err != nil {
		return err
	}

	fmt.Println(i18n.T("star_done", record.Name))

	if s.Snapshot(record.ID) {
		fmt.Println(i18n.T("star_snapshot"))
	}

	return nil
}

func info(s *store.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: starred info <id>")
	}

	var transcript claude.Transcript
	transcriptPath, err := claude.FindTranscript(args[0])

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err == nil {
		if transcript, err = claude.ReadTranscript(transcriptPath); err != nil {
			return err
		}
	}

	status := i18n.T("star_status_no")

	if s.Find(args[0]) != nil {
		status = i18n.T("star_status_yes")
	}

	fmt.Printf("status: %s\nname: %s\nsuggestion: %s\n", status, currentName(s, args[0], transcript), transcript.AITitle)

	return nil
}

func currentName(s *store.Store, id string, transcript claude.Transcript) string {
	if record := s.Find(id); record != nil {
		return record.Name
	}

	return transcript.CustomTitle
}

func install() error {
	executable, err := executablePath()

	if err != nil {
		return err
	}

	if err := setup.InstallSkill(executable); err != nil {
		return err
	}

	fmt.Printf("Installed the /star skill: %s\nRun `starred` to browse starred sessions.\n", setup.SkillDir())

	return nil
}

func uninstall() error {
	question := "Remove the /star skill, all starred data and this binary? Claude sessions are not touched. [y/N] "
	s, err := store.Open(store.DefaultDir())

	if err != nil {
		question = fmt.Sprintf("Cannot read the starred list (%v); kept transcripts in %s will be deleted.\n%s", err, store.DefaultDir(), question)
	} else if onlyCopies := countOnlyCopies(s); onlyCopies > 0 {
		question = fmt.Sprintf("%d starred sessions were already deleted by Claude and exist only here; they will be lost.\n%s", onlyCopies, question)
	}

	fmt.Print(question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')

	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Cancelled")
		return nil
	}

	if err := setup.RemoveSkill(); err != nil {
		return err
	}

	if err := os.RemoveAll(store.DefaultDir()); err != nil {
		return err
	}

	executable, err := executablePath()

	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		fmt.Printf("Done. Delete %s manually.\n", executable)
		return nil
	}

	fmt.Println("Done.")

	return os.Remove(executable)
}

func countOnlyCopies(s *store.Store) int {
	count := 0

	for _, record := range s.Records {
		if s.OnlyCopy(record.ID) {
			count++
		}
	}

	return count
}

func update() error {
	executable, err := executablePath()

	if err != nil {
		return err
	}

	tag, updated, err := setup.Update(version, executable)

	if err != nil {
		return err
	}

	if updated && setup.SkillInstalled() {
		if output, err := exec.Command(executable, "install").CombinedOutput(); err != nil {
			return fmt.Errorf("binary updated to %s, but the /star skill was not refreshed (%w: %s); run `starred install`", tag, err, output)
		}
	}

	fmt.Println("starred", tag)

	return nil
}

func executablePath() (string, error) {
	executable, err := os.Executable()

	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(executable)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
