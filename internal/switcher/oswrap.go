package switcher

import "os"

func osStat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func errNotExist() error {
	return os.ErrNotExist
}
