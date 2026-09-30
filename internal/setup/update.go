package setup

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repository = "artengin/claude-starred"

var httpClient = &http.Client{Timeout: 2 * time.Minute}

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func Update(currentVersion, executable string) (string, bool, error) {
	if currentVersion == "dev" {
		return "", false, errors.New("this is a development build; update it with `go install` or reinstall from a release")
	}

	latest, err := latestRelease()

	if err != nil {
		return "", false, err
	}

	if strings.TrimPrefix(latest.Tag, "v") == strings.TrimPrefix(currentVersion, "v") {
		return latest.Tag, false, nil
	}

	archiveName := fmt.Sprintf("claude-starred_%s_%s.%s", runtime.GOOS, runtime.GOARCH, archiveExtension())
	archive, err := download(latest.assetURL(archiveName))

	if err != nil {
		return "", false, err
	}

	checksums, err := download(latest.assetURL("checksums.txt"))

	if err != nil {
		return "", false, err
	}

	if err := verifyChecksum(archive, checksums, archiveName); err != nil {
		return "", false, err
	}

	binary, err := extractBinary(archive)

	if err != nil {
		return "", false, err
	}

	if err := replaceExecutable(executable, binary); err != nil {
		return "", false, err
	}

	return latest.Tag, true, nil
}

func latestRelease() (release, error) {
	var latest release
	data, err := download("https://api.github.com/repos/" + repository + "/releases/latest")

	if err != nil {
		return latest, err
	}

	return latest, json.Unmarshal(data, &latest)
}

func (r release) assetURL(name string) string {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset.URL
		}
	}

	return ""
}

func download(url string) ([]byte, error) {
	if url == "" {
		return nil, errors.New("release asset not found for this platform")
	}

	response, err := httpClient.Get(url)

	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, response.Status)
	}

	return io.ReadAll(response.Body)
}

func verifyChecksum(archive, checksums []byte, archiveName string) error {
	sum := sha256.Sum256(archive)
	scanner := bufio.NewScanner(bytes.NewReader(checksums))

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		if len(fields) == 2 && fields[1] == archiveName {
			if fields[0] == hex.EncodeToString(sum[:]) {
				return nil
			}

			return errors.New("checksum mismatch for " + archiveName)
		}
	}

	return errors.New("no checksum for " + archiveName)
}

func extractBinary(archive []byte) ([]byte, error) {
	name := binaryName()

	if runtime.GOOS == "windows" {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))

		if err != nil {
			return nil, err
		}

		for _, file := range reader.File {
			if filepath.Base(file.Name) == name {
				content, err := file.Open()

				if err != nil {
					return nil, err
				}

				defer content.Close()

				return io.ReadAll(content)
			}
		}

		return nil, errors.New(name + " not found in archive")
	}

	decompressed, err := gzip.NewReader(bytes.NewReader(archive))

	if err != nil {
		return nil, err
	}

	reader := tar.NewReader(decompressed)

	for {
		header, err := reader.Next()

		if err != nil {
			return nil, errors.New(name + " not found in archive")
		}

		if filepath.Base(header.Name) == name {
			return io.ReadAll(reader)
		}
	}
}

func replaceExecutable(executable string, binary []byte) error {
	temporary := executable + ".new"

	if err := os.WriteFile(temporary, binary, 0o755); err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		return os.Rename(temporary, executable)
	}

	backup := executable + ".old"
	os.Remove(backup)

	if err := os.Rename(executable, backup); err != nil {
		return err
	}

	if err := os.Rename(temporary, executable); err != nil {
		if restoreErr := os.Rename(backup, executable); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("%s was not restored, rename %s back to it: %w", executable, backup, restoreErr))
		}

		return err
	}

	return nil
}

func archiveExtension() string {
	if runtime.GOOS == "windows" {
		return "zip"
	}

	return "tar.gz"
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "claude-starred.exe"
	}

	return "claude-starred"
}
