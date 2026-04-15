package gateway

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// portHolderHint returns a human-readable suffix identifying which process is
// holding the given address, if we can figure it out. It's best-effort — any
// failure returns "" so the original bind error stays clean.
//
// Format: "\n  hint: port held by PID <n> (<name>). kill it with: <cmd>"
func portHolderHint(addr string) string {
	// addr is "host:port"; we only need the port for the lookup.
	_, port, ok := strings.Cut(addr, ":")
	if !ok || port == "" {
		return ""
	}

	pid, name := findPortHolder(port)
	if pid == "" {
		return ""
	}

	var killCmd string
	switch runtime.GOOS {
	case "windows":
		killCmd = "taskkill /PID " + pid + " /F"
	default:
		killCmd = "kill " + pid
	}
	if name != "" {
		return fmt.Sprintf("\n  hint: port %s is held by PID %s (%s). Kill it with: %s", port, pid, name, killCmd)
	}
	return fmt.Sprintf("\n  hint: port %s is held by PID %s. Kill it with: %s", port, pid, killCmd)
}

// findPortHolder returns (pid, processName) for whatever is LISTENING on port.
// Either or both can be "" if not found. Uses platform-native tools to avoid
// pulling in extra dependencies for a diagnostic code path.
func findPortHolder(port string) (pid, name string) {
	switch runtime.GOOS {
	case "windows":
		return findPortHolderWindows(port)
	default:
		return findPortHolderUnix(port)
	}
}

// netstatLineRE matches the PID at the end of a Windows netstat -ano LISTENING
// row, e.g. "  TCP    127.0.0.1:8642   0.0.0.0:0   LISTENING   48972".
var netstatLineRE = regexp.MustCompile(`LISTENING\s+(\d+)\s*$`)

func findPortHolderWindows(port string) (string, string) {
	out, err := exec.Command("netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return "", ""
	}
	needle := ":" + port
	for _, line := range strings.Split(string(out), "\n") {
		// Match "127.0.0.1:<port>" or "0.0.0.0:<port>" in the LocalAddress column.
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		// Ensure the port isn't a prefix match like :86420 when we wanted :8642.
		after := line[idx+len(needle):]
		if after != "" && after[0] >= '0' && after[0] <= '9' {
			continue
		}
		m := netstatLineRE.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		pid := m[1]
		return pid, tasklistName(pid)
	}
	return "", ""
}

// tasklistNameRE extracts the image name from "tasklist /FI PID eq N /FO CSV /NH"
// output, e.g. `"pan-agent.exe","48972","Console","1","12,345 K"`.
var tasklistNameRE = regexp.MustCompile(`^"([^"]+)"`)

func tasklistName(pid string) string {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	m := tasklistNameRE.FindStringSubmatch(line)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func findPortHolderUnix(port string) (string, string) {
	// Prefer lsof — it's near-universal on macOS and usually present on Linux.
	// -nP avoids DNS/service-name lookups; -sTCP:LISTEN narrows to listeners;
	// -Fpc asks for machine-readable "p<pid>\nc<command>" output.
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return "", ""
	}
	var pid, name string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "p") {
			pid = strings.TrimPrefix(line, "p")
		} else if strings.HasPrefix(line, "c") {
			name = strings.TrimPrefix(line, "c")
		}
	}
	return pid, name
}
