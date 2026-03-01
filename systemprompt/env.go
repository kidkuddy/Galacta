package systemprompt

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EnvContext holds environment information injected into the system prompt.
type EnvContext struct {
	OS          string // runtime.GOOS
	Shell       string // $SHELL
	HomeDir     string
	WorkingDir  string
	Platform    string // e.g., "darwin", "linux"
	OSVersion   string // uname -r output
	IsGitRepo   bool
	GitBranch   string
	GitStatus   string // short status output
	GitRecent   string // recent commit log
	Model       string
}

// CollectEnv gathers environment context for the system prompt.
func CollectEnv(workingDir, model string) EnvContext {
	env := EnvContext{
		OS:         runtime.GOOS,
		Platform:   runtime.GOOS,
		Shell:      os.Getenv("SHELL"),
		WorkingDir: workingDir,
		Model:      model,
	}

	env.HomeDir, _ = os.UserHomeDir()

	// OS version via uname
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		env.OSVersion = strings.TrimSpace(string(out))
	}

	// Git info
	env.IsGitRepo = isGitRepo(workingDir)
	if env.IsGitRepo {
		env.GitBranch = gitCommand(workingDir, "rev-parse", "--abbrev-ref", "HEAD")
		env.GitStatus = gitCommand(workingDir, "status", "--short")
		env.GitRecent = gitCommand(workingDir, "log", "--oneline", "-5")
	}

	return env
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func gitCommand(dir string, args ...string) string {
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
