package switcher

import (
	"os"
	"path/filepath"
)

// Paths defines the live Factory home and switcher-owned storage layout.
type Paths struct {
	Home        string
	Store       string
	Accounts    string
	ActiveFile  string
	FactoryHome string
}

// DefaultPaths builds the storage layout for the current operating-system user.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return NewPaths(home), nil
}

// NewPaths builds the storage layout beneath an explicit home directory.
func NewPaths(home string) Paths {
	store := filepath.Join(home, appDirName)
	return Paths{
		Home:        home,
		Store:       store,
		Accounts:    filepath.Join(store, "accounts"),
		ActiveFile:  filepath.Join(store, activeFileName),
		FactoryHome: filepath.Join(home, factoryDirName),
	}
}

// AccountFactoryHome returns the isolated Factory home for a saved account.
func (p Paths) AccountFactoryHome(name string) string {
	return filepath.Join(p.Accounts, name, factoryDirName)
}

// FactorySessions returns the live Factory sessions directory.
func (p Paths) FactorySessions() string {
	return filepath.Join(p.FactoryHome, "sessions")
}
