package switcher

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func copyDirFiles(srcDir, dstDir string, mode os.FileMode, files []string) error {
	for _, file := range files {
		if err := copyFile(filepath.Join(srcDir, file), filepath.Join(dstDir, file), mode); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func sameContent(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	if len(ab) != len(bb) {
		return false, nil
	}
	for i := range ab {
		if ab[i] != bb[i] {
			return false, nil
		}
	}
	return true, nil
}

func writerIsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		info, err := f.Stat()
		if err == nil {
			return (info.Mode() & os.ModeCharDevice) != 0
		}
	}
	return false
}

func atomicCopy(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWriteFile(dst, data, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp-" + randomSuffix()
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
