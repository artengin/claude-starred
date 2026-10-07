package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/artengin/claude-starred/internal/claude"
)

const starTemplate = `---
name: star
description: Add the current session to starred sessions (claude-starred) and keep it safe from automatic cleanup.
disable-model-invocation: true
allowed-tools: Bash(%[1]s info:*), Bash(%[1]s star:*)
---

` + skillMarker + `

Current state of this session:

!` + "`%[1]s info ${CLAUDE_SESSION_ID}`" + `

Talk to the user in their language.

If the user passed a name with the command ($ARGUMENTS), use it as the name and go to step 3 without asking.

1. If ` + "`name`" + ` above is empty, ask the user for a session name with AskUserQuestion. When ` + "`suggestion`" + ` is not empty, offer it as the first option; the user can always type their own name.
2. If ` + "`name`" + ` is not empty, ask with AskUserQuestion whether to keep «name» or rename the session, and ask for the new name if they choose to rename.
3. Run this command exactly, putting the chosen name as is on the middle line. Never put the name anywhere else in the command:

` + "```" + `
%[1]s star ${CLAUDE_SESSION_ID} --name - <<'STARRED_NAME'
<name>
STARRED_NAME
` + "```" + `

4. Reply with the command output only.
`

const unstarTemplate = `---
name: unstar
description: Remove the current session from starred sessions (claude-starred).
disable-model-invocation: true
allowed-tools: Bash(%[1]s unstar:*)
---

` + skillMarker + `

Run this command exactly:

` + "```" + `
%[1]s unstar ${CLAUDE_SESSION_ID}
` + "```" + `

Reply with the command output only.
`

const skillMarker = "<!-- installed by claude-starred; edits are overwritten on update -->"

const legacyStarDescription = "description: Add the current session to starred sessions (claude-starred) and keep it safe from automatic cleanup."

type skill struct {
	name     string
	template string
	required bool
}

type skillState int

const (
	absentSkill skillState = iota
	ownSkill
	foreignSkill
)

var skills = []skill{
	{name: "star", template: starTemplate, required: true},
	{name: "unstar", template: unstarTemplate},
}

func SkillsDir() string {
	return filepath.Join(claude.Dir(), "skills")
}

func InstallSkills(executable string) (installed, skipped []string, err error) {
	command, err := skillCommand(executable)

	if err != nil {
		return nil, nil, err
	}

	states := make([]skillState, len(skills))

	for i, s := range skills {
		states[i], err = s.state()

		if s.required && err != nil {
			return nil, nil, err
		}

		if s.required && states[i] == foreignSkill {
			return nil, nil, fmt.Errorf("%s already exists and belongs to another skill", s.file())
		}

		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", s.file(), err))
			states[i] = foreignSkill
		} else if states[i] == foreignSkill {
			skipped = append(skipped, s.file()+" belongs to another skill")
		}
	}

	for i, s := range skills {
		if states[i] == foreignSkill {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(s.file()), 0o755); err != nil {
			return installed, skipped, err
		}

		if err := os.WriteFile(s.file(), []byte(fmt.Sprintf(s.template, command)), 0o644); err != nil {
			return installed, skipped, err
		}

		installed = append(installed, s.name)
	}

	return installed, skipped, nil
}

func skillCommand(executable string) (string, error) {
	if onPath, err := exec.LookPath(filepath.Base(executable)); err == nil && sameFile(onPath, executable) {
		return filepath.Base(executable), nil
	}

	if strings.ContainsAny(filepath.ToSlash(executable), "'`()$\\\"") {
		return "", fmt.Errorf("%s contains characters that break the skill; move the binary to a plain path or add it to PATH", executable)
	}

	return shellQuote(executable), nil
}

func sameFile(a, b string) bool {
	first, err := os.Stat(a)

	if err != nil {
		return false
	}

	second, err := os.Stat(b)

	return err == nil && os.SameFile(first, second)
}

func SkillInstalled() bool {
	for _, s := range skills {
		if !s.required {
			continue
		}

		if state, err := s.state(); err != nil || state != ownSkill {
			return false
		}
	}

	return true
}

func RemoveSkills() error {
	for _, s := range skills {
		state, err := s.state()

		if err != nil {
			return err
		}

		if state != ownSkill {
			continue
		}

		if err := os.RemoveAll(filepath.Dir(s.file())); err != nil {
			return err
		}
	}

	return nil
}

func (s skill) file() string {
	return filepath.Join(SkillsDir(), s.name, "SKILL.md")
}

func (s skill) state() (skillState, error) {
	data, err := os.ReadFile(s.file())

	if errors.Is(err, os.ErrNotExist) {
		return absentSkill, nil
	}

	if err != nil {
		return absentSkill, err
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	if strings.Contains(content, skillMarker) || strings.Contains(content, legacyStarDescription+"\n") {
		return ownSkill, nil
	}

	return foreignSkill, nil
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", `'\''`) + "'"
}
