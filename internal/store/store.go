package store

import (
	"bufio"
	"bytes"
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
	if err := keepCopy(record.Transcript, s.CopyPath(record.ID), s.HistoryPath(record.ID)); err != nil {
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
	err := s.modify(func(records []Record) ([]Record, error) {
		var kept []Record

		for _, record := range records {
			if record.ID != id {
				kept = append(kept, record)
			}
		}

		return kept, nil
	})

	if err != nil {
		return err
	}

	for _, path := range []string{s.CopyPath(id), s.HistoryPath(id)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
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
		if err := s.SyncRecord(record); err != nil {
			errs = append(errs, fmt.Errorf("«%s»: %w", record.Name, err))
		}
	}

	return errors.Join(errs...)
}

func (s *Store) SyncRecord(record Record) error {
	copyPath := s.CopyPath(record.ID)

	if exists(record.Transcript) {
		return keepCopy(record.Transcript, copyPath, s.HistoryPath(record.ID))
	}

	if !exists(copyPath) {
		return nil
	}

	return restore(copyPath, s.HistoryPath(record.ID), record.Transcript)
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

func (s *Store) HistoryPath(id string) string {
	return filepath.Join(s.Dir, "transcripts", id+".history.jsonl")
}

func (s *Store) file() string {
	return filepath.Join(s.Dir, "starred.json")
}

func (s *Store) modify(change func([]Record) ([]Record, error)) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(s.Dir, "starred.lock"), os.O_CREATE|os.O_RDWR, 0o600)

	if err != nil {
		return err
	}

	defer lock.Close()

	if err := lockFile(lock); err != nil {
		return err
	}

	defer func() { _ = unlockFile(lock) }()

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

	return writeAndRename(s.file(), func(output io.Writer) error {
		_, err := output.Write(append(data, '\n'))

		return err
	})
}

func writeAndRename(target string, write func(io.Writer) error) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")

	if err != nil {
		return err
	}

	err = write(temporary)

	if err == nil {
		err = temporary.Sync()
	}

	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(temporary.Name(), target)
	}

	if err != nil {
		os.Remove(temporary.Name())
	}

	return err
}

func keepCopy(source, target, history string) error {
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

		continues, err := startsWith(source, target)

		if err != nil {
			return err
		}

		if !continues {
			if err := archive(target, history); err != nil {
				return err
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}

	os.Remove(target)

	if os.Link(source, target) == nil {
		return nil
	}

	return copyFile(source, target, sourceInfo.ModTime(), false)
}

func restore(copyPath, history, transcript string) error {
	if exists(history) {
		if err := writeAndRename(copyPath, func(output io.Writer) error {
			return concatenate(output, history, copyPath)
		}); err != nil {
			return err
		}

		if err := os.Remove(history); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		return err
	}

	err := os.Link(copyPath, transcript)

	if errors.Is(err, os.ErrExist) {
		return nil
	}

	if err != nil {
		if err := copyFile(copyPath, transcript, time.Now(), true); err != nil {
			if errors.Is(err, os.ErrExist) {
				return nil
			}

			return err
		}
	}

	now := time.Now()

	return os.Chtimes(transcript, now, now)
}

func archive(path, history string) error {
	previous := ""

	if exists(history) {
		previous = history
	}

	return writeAndRename(history, func(output io.Writer) error {
		return concatenate(output, previous, path)
	})
}

func concatenate(output io.Writer, paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}

		data, err := os.ReadFile(path)

		if err != nil {
			return err
		}

		if cut := bytes.LastIndexByte(data, '\n'); cut >= 0 {
			data = data[:cut+1]
		} else {
			continue
		}

		if _, err := output.Write(data); err != nil {
			return err
		}
	}

	return nil
}

func startsWith(path, prefixPath string) (bool, error) {
	info, err := os.Stat(path)

	if err != nil {
		return false, err
	}

	prefixInfo, err := os.Stat(prefixPath)

	if err != nil {
		return false, err
	}

	if info.Size() < prefixInfo.Size() {
		return false, nil
	}

	file, err := os.Open(path)

	if err != nil {
		return false, err
	}

	defer file.Close()

	prefix, err := os.Open(prefixPath)

	if err != nil {
		return false, err
	}

	defer prefix.Close()

	head := bufio.NewReader(io.LimitReader(file, prefixInfo.Size()))
	expected := bufio.NewReader(prefix)

	for {
		want, err := expected.ReadByte()

		if errors.Is(err, io.EOF) {
			return true, nil
		}

		if err != nil {
			return false, err
		}

		got, err := head.ReadByte()

		if err != nil || got != want {
			return false, nil
		}
	}
}

func copyFile(source, target string, modTime time.Time, exclusive bool) error {
	input, err := os.Open(source)

	if err != nil {
		return err
	}

	defer input.Close()

	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC

	if exclusive {
		flags = os.O_CREATE | os.O_WRONLY | os.O_EXCL
	}

	output, err := os.OpenFile(target, flags, 0o600)

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
