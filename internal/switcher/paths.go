package switcher

import (
	"os"
	"path/filepath"
)

type Paths struct {
	Home        string
	Store       string
	Accounts    string
	ActiveFile  string
	FactoryHome string
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	return NewPaths(home), nil
}

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

func (p Paths) AccountFactoryHome(name string) string {
	return filepath.Join(p.Accounts, name, factoryDirName)
}
