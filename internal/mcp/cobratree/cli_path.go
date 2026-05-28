
package cobratree

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// SiblingCLIPath resolves the companion CLI via sibling-of-executable,
// TODOIST_CLI_PATH env var, then PATH.
func SiblingCLIPath() (string, error) {
	cliName := cliExecutableName(runtime.GOOS)
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), cliName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if v := os.Getenv("TODOIST_CLI_PATH"); v != "" {
		return v, nil
	}
	return exec.LookPath(cliName)
}

func cliExecutableName(goos string) string {
	name := "todoist-aum"
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}
