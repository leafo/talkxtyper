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

func TestNormalizeTranscriptionProvider(t *testing.T) {
	tests := []struct {
		input TranscriptionProvider
		want  TranscriptionProvider
	}{
		{"", TranscriptionProviderOpenAI},
		{"unknown", TranscriptionProviderOpenAI},
		{TranscriptionProviderOpenAI, TranscriptionProviderOpenAI},
		{TranscriptionProviderGemini, TranscriptionProviderGemini},
	}

	for _, test := range tests {
		if got := normalizeTranscriptionProvider(test.input); got != test.want {
			t.Errorf("normalizeTranscriptionProvider(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
