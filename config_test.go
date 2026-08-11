package main

import "testing"

func TestNormalizeTranscriptionMode(t *testing.T) {
	tests := []struct {
		input TranscriptionMode
		want  TranscriptionMode
	}{
		{"", TranscriptionModeBuffered},
		{"unknown", TranscriptionModeBuffered},
		{TranscriptionModeBuffered, TranscriptionModeBuffered},
		{TranscriptionModeLive, TranscriptionModeLive},
	}

	for _, test := range tests {
		if got := normalizeTranscriptionMode(test.input); got != test.want {
			t.Errorf("normalizeTranscriptionMode(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
