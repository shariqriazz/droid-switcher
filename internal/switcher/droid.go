package switcher

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

type DroidRunner struct {
	Path   string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

var runDroid = func(r DroidRunner, factoryHome string, args ...string) error {
	return r.Run(factoryHome, args...)
}

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

func (r DroidRunner) Run(factoryHome string, args ...string) error {
	cmd := exec.Command(r.Path, args...)
	cmd.Env = append(os.Environ(), "FACTORY_HOME_OVERRIDE="+factoryHome)
	cmd.Stdin = r.Stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("droid exited with error: %w", err)
	}
	return nil
}
