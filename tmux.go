package main

import (
	"fmt"
	"os/exec"
	"strings"
)

type TmuxPane struct {
	SessionName     string
	WindowIndex     string
	WindowName      string
	PaneIndex       string
	PaneID          string
	PanePid         string
	CurrentCommand  string
	WindowActive    bool
	PaneActive      bool
	SessionAttached bool
}

const tmuxPaneFormat = "#{session_name}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{window_active}\t#{pane_active}\t#{session_attached}"

// ListTmuxPanes returns every pane across every session. Returns (nil, nil) if
// the tmux server is not running.
func ListTmuxPanes() ([]TmuxPane, error) {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", tmuxPaneFormat)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "no server running") {
				return nil, nil
			}
			return nil, fmt.Errorf("tmux list-panes: %s", strings.TrimSpace(stderr))
		}
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	var panes []TmuxPane
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 10 {
			continue
		}
		panes = append(panes, TmuxPane{
			SessionName:     fields[0],
			WindowIndex:     fields[1],
			WindowName:      fields[2],
			PaneIndex:       fields[3],
			PaneID:          fields[4],
			PanePid:         fields[5],
			CurrentCommand:  fields[6],
			WindowActive:    fields[7] == "1",
			PaneActive:      fields[8] == "1",
			SessionAttached: fields[9] == "1",
		})
	}
	return panes, nil
}

// FindActiveTmuxPane returns the pane being shown by the tmux client running
// inside the X11 active window, or an error if no such client exists.
func FindActiveTmuxPane() (*TmuxPane, error) {
	activePid := ActiveWindowPid()
	if activePid == "" {
		return nil, fmt.Errorf("no active window")
	}

	cmd := exec.Command("tmux", "list-clients", "-F", "#{client_pid}\t#{client_session}")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "no server running") {
				return nil, fmt.Errorf("tmux server not running")
			}
			return nil, fmt.Errorf("tmux list-clients: %s", strings.TrimSpace(stderr))
		}
		return nil, fmt.Errorf("tmux list-clients: %w", err)
	}

	var activeSession string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		clientPid, session := fields[0], fields[1]
		if hasAncestor(clientPid, activePid) {
			activeSession = session
			break
		}
	}
	if activeSession == "" {
		return nil, fmt.Errorf("no tmux client under active window PID %s", activePid)
	}

	panes, err := ListTmuxPanes()
	if err != nil {
		return nil, err
	}
	for i := range panes {
		p := &panes[i]
		if p.SessionName == activeSession && p.WindowActive && p.PaneActive {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no active pane in session %s", activeSession)
}

// CapturePane returns the contents of the pane. historyLines is the number of
// scrollback lines to include above the visible region (0 = visible only).
func CapturePane(paneID string, historyLines int) (string, error) {
	args := []string{"capture-pane", "-p", "-t", paneID}
	if historyLines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", historyLines))
	}
	cmd := exec.Command("tmux", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("tmux capture-pane: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	return string(output), nil
}

// BuildTranscriptionContext returns the prompt fragment for this pane's
// visible buffer, wrapped with descriptive context.
func (p *TmuxPane) BuildTranscriptionContext() (string, error) {
	buffer, err := CapturePane(p.PaneID, 0)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"The user is in a terminal (tmux session %q, window %s, pane %s, command %s) with the following visible content:\n%s",
		p.SessionName, p.WindowIndex, p.PaneIndex, p.CurrentCommand, buffer,
	), nil
}

// BuildTmuxTranscriptionContext finds the active tmux pane and returns its
// transcription context fragment.
func BuildTmuxTranscriptionContext() (string, error) {
	pane, err := FindActiveTmuxPane()
	if err != nil {
		return "", err
	}
	return pane.BuildTranscriptionContext()
}
