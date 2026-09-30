package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/artengin/claude-starred/internal/claude"
)

const skillMarker = "claude-starred"

const skillTemplate = `---
name: star
description: Add the current session to starred sessions (claude-starred) and keep it safe from automatic cleanup.
disable-model-invocation: true
allowed-tools: Bash(%[1]s info:*), Bash(%[1]s star:*)
---

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

func SkillDir() string {
	return filepath.Join(claude.Dir(), "skills", "star")
}

func InstallSkill(executable string) error {
	path := filepath.Join(SkillDir(), "SKILL.md")

	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), skillMarker) {
		return fmt.Errorf("%s already exists and belongs to another skill", path)
	}

	if err := os.MkdirAll(SkillDir(), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(fmt.Sprintf(skillTemplate, shellQuote(executable))), 0o644)
}

func SkillInstalled() bool {
	data, err := os.ReadFile(filepath.Join(SkillDir(), "SKILL.md"))

	return err == nil && strings.Contains(string(data), skillMarker)
}

func RemoveSkill() error {
	data, err := os.ReadFile(filepath.Join(SkillDir(), "SKILL.md"))

	if errors.Is(err, os.ErrNotExist) || (err == nil && !strings.Contains(string(data), skillMarker)) {
		return nil
	}

	if err != nil {
		return err
	}

	return os.RemoveAll(SkillDir())
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(path), "'", `'\''`) + "'"
}
