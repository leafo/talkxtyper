package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
)

var getInsertionTextCmd = template.Must(template.New("getInsertionTextCmd").Parse(`
	local mode = vim.api.nvim_get_mode()["mode"]
	local num_lines = {{.NumLines}}
	local sigil = "{{.Sigil}}"

	if mode == "i" then
		local current_line = vim.api.nvim_win_get_cursor(0)[1]
		local total_lines = vim.api.nvim_buf_line_count(0)

		local start_line = math.max(1, current_line - num_lines)
		local end_line = math.min(total_lines, current_line + num_lines)

		local before = vim.api.nvim_buf_get_lines(0, start_line-1, current_line - 1, false)
		local after = vim.api.nvim_buf_get_lines(0, current_line, end_line, false)

		-- get the current line in two parts split by the cursor position
		local cursor_pos = vim.api.nvim_win_get_cursor(0)[2]
		local current_line_text = vim.api.nvim_buf_get_lines(0, current_line-1, current_line, false)[1]

		local before_cursor = string.sub(current_line_text, 1, cursor_pos)
		local after_cursor = string.sub(current_line_text, cursor_pos + 1)

		-- join the lines with the cursor inserted in the middle
		local lines = {}

		for i, line in ipairs(before) do
			table.insert(lines, line)
		end

		table.insert(lines, before_cursor .. sigil .. after_cursor)

		for i, line in ipairs(after) do
			table.insert(lines, line)
		end

		return table.concat(lines, "\n")
	end

	return "" -- don't want to type anything strange when in another mode
`))

// get text across all visible buffers
var getVisibleTextCmd = template.Must(template.New("getVisibleTextCmd").Parse(`
	-- backticks can't be escaped in raw go string literal
	local three_ticks = string.rep(string.char(96), 3)
	local CONTEXT_EXTEND = 20

	-- This generates a context string of the currently visible text in the
	-- specified win_id (or the current window if none is specified)
	-- Format:
	-- filename:start_line-end_line
	-- {visible_lines}
	local function get_context(win_id)
		if not win_id then
			win_id = 0
		end

		local out

		vim.api.nvim_win_call(win_id, function()
			local filename = vim.fn.expand("%")
			local first_visible = math.max(1, vim.fn.line("w0") - CONTEXT_EXTEND)
			local last_visible = math.min(vim.fn.line("$"), vim.fn.line("w$") + CONTEXT_EXTEND)

			if first_visible < 20 then
				first_visible = 1
			end

			local visible_lines = vim.api.nvim_buf_get_lines(0, first_visible - 1, last_visible, false)
			local header = string.format("%s:%d-%d", filename, first_visible, last_visible)

			out = "START " .. header .. "\n" .. three_ticks .. "\n" .. table.concat(visible_lines, "\n") .. "\n" .. three_ticks .. "\nEND " .. header
		end)

		return out
	end

	local contexts = {}
	for _, win_id in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
		table.insert(contexts, get_context(win_id))
	end

	return table.concat(contexts, "\n")
`))

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

func (client *NvimClient) RemoteExecuteLua(command string) (string, error) {
	command = strings.ReplaceAll(command, "'", "''")
	luaCommand := fmt.Sprintf("luaeval('(function() %s end)()')", command)
	return client.RemoteExecute(luaCommand)
}

// Returns the text in nvim surrounding the cursor when in insertion mode
func (client *NvimClient) GetInsertionText(cursorSigil string) (string, error) {
	var insertionTextCmd strings.Builder
	err := getInsertionTextCmd.Execute(&insertionTextCmd, map[string]interface{}{
		"NumLines": 20,
		"Sigil":    cursorSigil,
	})
	if err != nil {
		return "", err
	}

	insertionTextOutput, err := client.RemoteExecuteLua(insertionTextCmd.String())
	if err != nil {
		return "", err
	}

	return insertionTextOutput, nil
}

// Returns all the visible text in the current nvim window
func (client *NvimClient) GetVisibleText() (string, error) {
	var visibleTextCmd strings.Builder
	err := getVisibleTextCmd.Execute(&visibleTextCmd, nil)
	if err != nil {
		return "", err
	}

	command := visibleTextCmd.String()

	output, err := client.RemoteExecuteLua(command)
	if err != nil {
		return "", err
	}

	return output, nil
}
