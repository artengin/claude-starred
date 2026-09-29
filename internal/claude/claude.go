package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Transcript struct {
	Cwd         string
	CustomTitle string
	AITitle     string
}

func Dir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}

	home, _ := os.UserHomeDir()

	return filepath.Join(home, ".claude")
}

func FindTranscript(sessionID string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(Dir(), "projects", "*", sessionID+".jsonl"))

	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", os.ErrNotExist
	}

	return matches[0], nil
}

func ReadTranscript(path string) (Transcript, error) {
	file, err := os.Open(path)

	if err != nil {
		return Transcript{}, err
	}

	defer file.Close()

	var transcript Transcript
	reader := bufio.NewReaderSize(file, 1<<20)

	for {
		line, err := reader.ReadBytes('\n')

		if len(line) > 0 {
			applyRecord(&transcript, line)
		}

		if err != nil {
			break
		}
	}

	return transcript, nil
}

func applyRecord(transcript *Transcript, line []byte) {
	needsCwd := transcript.Cwd == "" && bytes.Contains(line, []byte(`"cwd"`))
	hasTitle := bytes.Contains(line, []byte(`"custom-title"`)) || bytes.Contains(line, []byte(`"ai-title"`))

	if !needsCwd && !hasTitle {
		return
	}

	var record struct {
		Type        string `json:"type"`
		Cwd         string `json:"cwd"`
		CustomTitle string `json:"customTitle"`
		AITitle     string `json:"aiTitle"`
	}

	if json.Unmarshal(line, &record) != nil {
		return
	}

	if needsCwd {
		transcript.Cwd = record.Cwd
	}

	switch record.Type {
	case "custom-title":
		transcript.CustomTitle = record.CustomTitle
	case "ai-title":
		transcript.AITitle = record.AITitle
	}
}

func LiveSessionIDs() map[string]bool {
	ids := map[string]bool{}
	paths, _ := filepath.Glob(filepath.Join(Dir(), "sessions", "*.json"))

	for _, path := range paths {
		var record struct {
			Pid       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}

		data, err := os.ReadFile(path)

		if err != nil || json.Unmarshal(data, &record) != nil {
			continue
		}

		if record.SessionID != "" && processAlive(record.Pid) {
			ids[record.SessionID] = true
		}
	}

	return ids
}

func ProjectRoot(cwd string) string {
	output, err := exec.Command("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()

	if err != nil {
		return cwd
	}

	commonDir := filepath.Clean(strings.TrimSpace(string(output)))

	if filepath.Base(commonDir) != ".git" {
		return cwd
	}

	return filepath.Dir(commonDir)
}
