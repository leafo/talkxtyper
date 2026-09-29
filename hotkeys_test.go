package main

import (
	"reflect"
	"testing"

	"golang.design/x/hotkey"
)

func TestParseHotkey(t *testing.T) {
	tests := []struct {
		input string
		mods  []hotkey.Modifier
		key   hotkey.Key
		label string
	}{
		{"Alt+B", []hotkey.Modifier{hotkey.Mod1}, hotkey.Key(0x62), "Alt+B"},
		{"F1", nil, hotkey.Key(0xffbe), "F1"},
		{"f12", nil, hotkey.Key(0xffc9), "F12"},
		{"ctrl + shift + space", []hotkey.Modifier{hotkey.ModCtrl, hotkey.ModShift}, hotkey.Key(0x20), "Ctrl+Shift+Space"},
		{"Super+1", []hotkey.Modifier{hotkey.Mod4}, hotkey.Key(0x31), "Super+1"},
		{"Alt+Alt+Tab", []hotkey.Modifier{hotkey.Mod1}, hotkey.Key(0xff09), "Alt+Tab"},
	}

	for _, test := range tests {
		got, err := parseHotkey(test.input)
		if err != nil {
			t.Errorf("parseHotkey(%q) returned error: %v", test.input, err)
			continue
		}
		if !reflect.DeepEqual(got.Mods, test.mods) || got.Key != test.key || got.Label != test.label {
			t.Errorf("parseHotkey(%q) = %+v, want mods %v key %#x label %q", test.input, got, test.mods, test.key, test.label)
		}
	}
}

func TestParseHotkeyInvalid(t *testing.T) {
	for _, input := range []string{"", "Alt+", "Alt", "Hyper+B", "Alt+BB", "F0", "F36", "B+Alt"} {
		if _, err := parseHotkey(input); err == nil {
			t.Errorf("parseHotkey(%q) expected an error", input)
		}
	}
}
