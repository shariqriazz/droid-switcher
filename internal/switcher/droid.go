package switcher

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// DroidRunner describes a Droid child process and its standard streams.
type DroidRunner struct {
	Path   string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

var runDroid = func(r DroidRunner, factoryHome string, args ...string) error {
	return r.Run(factoryHome, args...)
}

// NewDroidRunner creates an interactive runner for the requested Droid binary.
func NewDroidRunner(path string) DroidRunner {
	if path == "" {
		path = "droid"
	}
	return DroidRunner{
		Path:   path,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Run launches Droid with an isolated Factory home and waits for it to exit.
func (r DroidRunner) Run(factoryHome string, args ...string) error {
	// r.Path is either the trusted default or an explicit --droid executable selected by the user.
	cmd := exec.Command(r.Path, args...) //nolint:gosec,noctx // The interactive child owns its lifetime and the executable is user-selected.
	cmd.Env = append(os.Environ(), "FACTORY_HOME_OVERRIDE="+droidOverrideHome(factoryHome))
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("droid exited with error: %w", err)
	}
	return nil
}

func droidOverrideHome(factoryHome string) string {
	clean := filepath.Clean(factoryHome)
	if filepath.Base(clean) == factoryDirName {
		return filepath.Dir(clean)
	}
	return factoryHome
}
