package main

import (
	"fmt"
	"strconv"
	"strings"

	"golang.design/x/hotkey"
)

const (
	DefaultRecordHotkey = "Alt+B"
	DefaultAbortHotkey  = "Alt+C"
)

// Hotkey is a parsed hotkey spec such as "Ctrl+Shift+F1".
type Hotkey struct {
	Mods  []hotkey.Modifier
	Key   hotkey.Key
	Label string
}

var hotkeyModifiers = map[string]struct {
	mod   hotkey.Modifier
	label string
}{
	"ctrl":    {hotkey.ModCtrl, "Ctrl"},
	"control": {hotkey.ModCtrl, "Ctrl"},
	"shift":   {hotkey.ModShift, "Shift"},
	"alt":     {hotkey.Mod1, "Alt"},
	"mod1":    {hotkey.Mod1, "Alt"},
	"super":   {hotkey.Mod4, "Super"},
	"win":     {hotkey.Mod4, "Super"},
	"mod4":    {hotkey.Mod4, "Super"},
}

// X11 keysyms are used directly because some of the library's constants are
// wrong (KeyTab duplicates KeyEscape and Key0-Key9 are off by one).
var hotkeyNamedKeys = map[string]struct {
	keysym uint16
	label  string
}{
	"space":      {0x0020, "Space"},
	"return":     {0xff0d, "Enter"},
	"enter":      {0xff0d, "Enter"},
	"escape":     {0xff1b, "Escape"},
	"esc":        {0xff1b, "Escape"},
	"tab":        {0xff09, "Tab"},
	"backspace":  {0xff08, "Backspace"},
	"delete":     {0xffff, "Delete"},
	"insert":     {0xff63, "Insert"},
	"home":       {0xff50, "Home"},
	"end":        {0xff57, "End"},
	"pageup":     {0xff55, "PageUp"},
	"pagedown":   {0xff56, "PageDown"},
	"left":       {0xff51, "Left"},
	"right":      {0xff53, "Right"},
	"up":         {0xff52, "Up"},
	"down":       {0xff54, "Down"},
	"pause":      {0xff13, "Pause"},
	"scrolllock": {0xff14, "ScrollLock"},
	"print":      {0xff61, "Print"},
}

// parseHotkey parses a "+"-separated spec of zero or more modifiers followed
// by a single key, case-insensitively, e.g. "Alt+B", "F1", "ctrl+shift+space".
func parseHotkey(spec string) (Hotkey, error) {
	parts := strings.Split(spec, "+")
	var result Hotkey
	var labels []string
	seen := map[hotkey.Modifier]bool{}

	for i, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			return Hotkey{}, fmt.Errorf("invalid hotkey %q: empty component", spec)
		}

		if i < len(parts)-1 {
			modifier, ok := hotkeyModifiers[name]
			if !ok {
				return Hotkey{}, fmt.Errorf("invalid hotkey %q: unknown modifier %q", spec, part)
			}
			if !seen[modifier.mod] {
				seen[modifier.mod] = true
				result.Mods = append(result.Mods, modifier.mod)
				labels = append(labels, modifier.label)
			}
			continue
		}

		keysym, label, err := parseHotkeyKey(name)
		if err != nil {
			return Hotkey{}, fmt.Errorf("invalid hotkey %q: %v", spec, err)
		}
		result.Key = hotkey.Key(keysym)
		labels = append(labels, label)
	}

	result.Label = strings.Join(labels, "+")
	return result, nil
}

func parseHotkeyKey(name string) (uint16, string, error) {
	if len(name) == 1 {
		c := name[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			// keysyms for lowercase letters and digits match ASCII
			return uint16(c), strings.ToUpper(name), nil
		}
	}

	if named, ok := hotkeyNamedKeys[name]; ok {
		return named.keysym, named.label, nil
	}

	if strings.HasPrefix(name, "f") {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 1 && n <= 35 {
			return uint16(0xffbe + n - 1), fmt.Sprintf("F%d", n), nil
		}
	}

	if _, isModifier := hotkeyModifiers[name]; isModifier {
		return 0, "", fmt.Errorf("missing key after modifier %q", name)
	}

	return 0, "", fmt.Errorf("unknown key %q", name)
}

// registerHotkey parses and registers a configured hotkey, falling back to
// defaultSpec when spec is empty. Failures are reported to the user and
// return nil.
func registerHotkey(description, spec, defaultSpec string) (*hotkey.Hotkey, string) {
	if strings.TrimSpace(spec) == "" {
		spec = defaultSpec
	}

	parsed, err := parseHotkey(spec)
	if err != nil {
		notifyError(fmt.Sprintf("Could not parse the %s hotkey", description), err)
		return nil, ""
	}

	hk := hotkey.New(parsed.Mods, parsed.Key)
	if err := hk.Register(); err != nil {
		notifyError(fmt.Sprintf("Could not register the %s hotkey (%s)", description, parsed.Label), err)
		return nil, ""
	}

	return hk, parsed.Label
}
