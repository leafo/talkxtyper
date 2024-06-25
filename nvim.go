package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type NvimClient struct {
	socketFile string
}

// NewNvimClient creates a new NvimClient for a given PID
func NewNvimClient() *NvimClient {
	return &NvimClient{}
}

// gets the default location for nvim process remote socket based on the PID
func findRemoteSocketFile(pid string) (string, error) {
	uid := os.Getuid()
	socketFile := fmt.Sprintf("/run/user/%d/nvim.%s.0", uid, pid)
	if _, err := os.Stat(socketFile); os.IsNotExist(err) {
		return "", fmt.Errorf("no nvim socket file found for PID %s, socket file: %s", pid, socketFile)
	}
	return socketFile, nil
}

// sets the socket to the active window if it's an nvim instance
func (client *NvimClient) FindActiveNvim() error {
	// find the PID of the active window in X
	cmd := exec.Command("sh", "-c", `xprop -root _NET_ACTIVE_WINDOW | awk '{print $5}' | xargs -I {} xprop -id {} _NET_WM_PID | awk '{print $3}'`)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	pid := strings.TrimSpace(string(output))

	if pid == "" {
		return fmt.Errorf("No active window found")
	}

	var searchPid func(string) (string, error)
	searchPid = func(currentPid string) (string, error) {
		socketFile, err := findRemoteSocketFile(currentPid)

		if err == nil {
			return socketFile, nil
		}

		cmd := exec.Command("sh", "-c", fmt.Sprintf(`pgrep -P %s`, currentPid))
		output, err := cmd.Output()
		if err != nil {
			return "", err
		}
		childPids := strings.Split(strings.TrimSpace(string(output)), "\n")

		for _, childPid := range childPids {
			socketFile, err = searchPid(childPid)
			if err == nil {
				return socketFile, nil
			}
		}
		return "", fmt.Errorf("No nvim process within PID %s", currentPid)
	}

	socketFile, err := searchPid(pid)
	if err == nil {
		client.socketFile = socketFile
		return nil
	}

	return fmt.Errorf("No nvim process found as a subprocess of PID %s", pid)
}

// find any running nvim server and set the socket file path
func (client *NvimClient) FindFirstNvim() error {
	cmd := exec.Command("sh", "-c", `pgrep -u $USER -x nvim`)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(pids) == 0 || pids[0] == "" {
		return fmt.Errorf("no running nvim instance found")
	}

	for _, pid := range pids {
		socketFile, err := findRemoteSocketFile(pid)
		if err == nil {
			client.socketFile = socketFile
			return nil
		}
	}

	return fmt.Errorf("no valid nvim socket file found")
}

func (client *NvimClient) RemoteExecute(command string) (string, error) {
	socketFile := client.socketFile
	if socketFile == "" {
		return "", fmt.Errorf("nvim socket not set")
	}

	fmt.Fprintf(os.Stderr, "nvim.RemoteExecute: using socket file: %s\n", socketFile)

	cmd := exec.Command("nvim", "--server", socketFile, "--remote-expr", command)
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command execution failed: %v, output: %s", err, cmdOutput)
	}

	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cleanOutput := ansiEscape.ReplaceAllString(string(cmdOutput), "")

	return cleanOutput, nil
}

func (client *NvimClient) GetVisibleText() (string, error) {
	visibleTextCmd := `join(getline('w0', line('.') - 1), "\n") . "{{CURSOR}}" . join(getline(line('.') + 1, 'w$'), "\n")`

	visibleTextOutput, err := client.RemoteExecute(visibleTextCmd)
	if err != nil {
		return "", err
	}

	return visibleTextOutput, nil
}
