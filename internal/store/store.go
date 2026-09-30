package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Record struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Cwd        string    `json:"cwd"`
	Project    string    `json:"project"`
	Transcript string    `json:"transcript"`
	StarredAt  time.Time `json:"starred_at"`
}

type Store struct {
	Dir     string
	Records []Record
}

func DefaultDir() string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "claude-starred")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "claude-starred")
	}

	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "claude-starred")
	}

	return filepath.Join(home, ".local", "share", "claude-starred")
}

func Open(dir string) (*Store, error) {
	store := &Store{Dir: dir}
	records, err := store.read()

	if err != nil {
		return nil, err
	}

	store.Records = records

	return store, nil
}

func (s *Store) Find(id string) *Record {
	for i := range s.Records {
		if s.Records[i].ID == id {
			return &s.Records[i]
		}
	}

	return nil
}

func (s *Store) Star(record Record) error {
	if err := keepCopy(record.Transcript, s.CopyPath(record.ID)); err != nil {
		return err
	}

	return s.modify(func(records []Record) ([]Record, error) {
		for i := range records {
			if records[i].ID == record.ID {
				record.StarredAt = records[i].StarredAt
				records[i] = record

				return records, nil
			}
		}

		record.StarredAt = time.Now()

		return append(records, record), nil
	})
}

func (s *Store) Rename(id, name string) error {
	return s.modify(func(records []Record) ([]Record, error) {
		for i := range records {
			if records[i].ID == id {
				records[i].Name = name

				return records, nil
			}
		}

		return nil, os.ErrNotExist
	})
}

func (s *Store) Unstar(id string) error {
	if err := os.Remove(s.CopyPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return s.modify(func(records []Record) ([]Record, error) {
		var kept []Record

		for _, record := range records {
			if record.ID != id {
				kept = append(kept, record)
			}
		}

		return kept, nil
	})
}

func (s *Store) Reload() error {
	records, err := s.read()

	if err != nil {
		return err
	}

	s.Records = records

	return nil
}

func (s *Store) Sync() error {
	var errs []error

	for _, record := range s.Records {
		if err := s.sync(record); err != nil {
			errs = append(errs, fmt.Errorf("«%s»: %w", record.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (s *Store) sync(record Record) error {
	copyPath := s.CopyPath(record.ID)

	if exists(record.Transcript) {
		return keepCopy(record.Transcript, copyPath)
	}

	if !exists(copyPath) {
		return nil
	}

	if err := keepCopy(copyPath, record.Transcript); err != nil {
		return err
	}

	now := time.Now()

	return os.Chtimes(record.Transcript, now, now)
}

func (s *Store) Lost(id string) bool {
	record := s.Find(id)

	return record != nil && !exists(record.Transcript) && !exists(s.CopyPath(id))
}

func (s *Store) OnlyCopy(id string) bool {
	record := s.Find(id)

	return record != nil && !exists(record.Transcript) && exists(s.CopyPath(id))
}

func (s *Store) Snapshot(id string) bool {
	record := s.Find(id)

	if record == nil {
		return false
	}

	transcript, err := os.Stat(record.Transcript)

	if err != nil {
		return false
	}

	copied, err := os.Stat(s.CopyPath(id))

	return err == nil && !os.SameFile(transcript, copied)
}

func (s *Store) CopyPath(id string) string {
	return filepath.Join(s.Dir, "transcripts", id+".jsonl")
}

func (s *Store) file() string {
	return filepath.Join(s.Dir, "starred.json")
}

func (s *Store) modify(change func([]Record) ([]Record, error)) error {
	records, err := s.read()

	if err != nil {
		return err
	}

	if records, err = change(records); err != nil {
		return err
	}

	if err := s.write(records); err != nil {
		return err
	}

	s.Records = records

	return nil
}

func (s *Store) read() ([]Record, error) {
	data, err := os.ReadFile(s.file())

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var records []Record

	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("%s is corrupted: %w", s.file(), err)
	}

	return records, nil
}

func (s *Store) write(records []Record) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(records, "", "  ")

	if err != nil {
		return err
	}

	temporary, err := os.CreateTemp(s.Dir, "starred.json.*.tmp")

	if err != nil {
		return err
	}

	if err := writeAndRename(temporary, append(data, '\n'), s.file()); err != nil {
		os.Remove(temporary.Name())
		return err
	}

	return nil
}

func writeAndRename(temporary *os.File, data []byte, target string) error {
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}

	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(temporary.Name(), target)
}

func keepCopy(source, target string) error {
	sourceInfo, err := os.Stat(source)

	if err != nil {
		return err
	}

	if targetInfo, err := os.Stat(target); err == nil {
		if os.SameFile(sourceInfo, targetInfo) {
			return nil
		}

		if targetInfo.ModTime().Equal(sourceInfo.ModTime()) && targetInfo.Size() == sourceInfo.Size() {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}

	os.Remove(target)

	if os.Link(source, target) == nil {
		return nil
	}

	return copyFile(source, target, sourceInfo.ModTime())
}

func copyFile(source, target string, modTime time.Time) error {
	input, err := os.Open(source)

	if err != nil {
		return err
	}

	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)

	if err != nil {
		return err
	}

	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}

	if err := output.Close(); err != nil {
		return err
	}

	return os.Chtimes(target, modTime, modTime)
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
