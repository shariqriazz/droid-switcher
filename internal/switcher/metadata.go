package switcher

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	accountMetaFileName = "account.json"
	defaultFileName     = "default"
)

type AccountMetadata struct {
	Label string `json:"label,omitempty"`
}

func accountRoot(p Paths, name string) string {
	return filepath.Join(p.Accounts, name)
}

func accountMetaPath(p Paths, name string) string {
	return filepath.Join(accountRoot(p, name), accountMetaFileName)
}

func defaultAccountPath(p Paths) string {
	return filepath.Join(p.Store, defaultFileName)
}

func loadAccountMetadata(p Paths, name string) (AccountMetadata, error) {
	data, err := os.ReadFile(accountMetaPath(p, name))
	if errors.Is(err, os.ErrNotExist) {
		return AccountMetadata{}, nil
	}
	if err != nil {
		return AccountMetadata{}, err
	}
	var meta AccountMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return AccountMetadata{}, err
	}
	meta.Label = strings.TrimSpace(meta.Label)
	return meta, nil
}

func saveAccountMetadata(p Paths, name string, meta AccountMetadata) error {
	meta.Label = strings.TrimSpace(meta.Label)
	if meta.Label == "" {
		if err := os.Remove(accountMetaPath(p, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(accountRoot(p, name), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(accountMetaPath(p, name), data, 0o600)
}

func CurrentDefaultAccount(p Paths) (string, bool, error) {
	data, err := os.ReadFile(defaultAccountPath(p))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	name := strings.TrimSpace(string(data))
	return name, name != "", nil
}

func writeDefaultAccount(p Paths, name string) error {
	if err := os.MkdirAll(p.Store, 0o700); err != nil {
		return err
	}
	return atomicWriteFile(defaultAccountPath(p), []byte(name+"\n"), 0o600)
}

func clearDefaultAccount(p Paths) error {
	if err := os.Remove(defaultAccountPath(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
