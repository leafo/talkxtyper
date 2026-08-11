package main

import (
	"slices"
	"testing"
)

func TestExtractTranscriptionKeywords(t *testing.T) {
	prompt := `The user is editing cmd/talkxtyper/main.go.
Keywords:
- Neovim
- OpenAI API
- invalid<keyword

func transcribeAudio() {
	request_id := "AC-42"
	HTTPServer := true
}`

	got := extractTranscriptionKeywords(prompt)
	want := []string{
		"Neovim",
		"OpenAI API",
		"cmd/talkxtyper/main.go",
		"transcribeAudio",
		"request_id",
		"AC-42",
		"HTTPServer",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("keywords = %#v, want %#v", got, want)
	}
}

func TestExtractTranscriptionKeywordsDeduplicatesCaseInsensitively(t *testing.T) {
	prompt := "Keywords:\n- OpenAI\n- openai\n\nOpenAI OpenAI"
	got := extractTranscriptionKeywords(prompt)
	want := []string{"OpenAI"}

	if !slices.Equal(got, want) {
		t.Fatalf("keywords = %#v, want %#v", got, want)
	}
}

func TestSanitizeTranscriptionKeyword(t *testing.T) {
	for _, invalid := range []string{"", "bad<term", "bad>term", "two\nlines", "two\rlines"} {
		if got := sanitizeTranscriptionKeyword(invalid); got != "" {
			t.Errorf("sanitizeTranscriptionKeyword(%q) = %q, want empty", invalid, got)
		}
	}

	if got := sanitizeTranscriptionKeyword("  - TalkXTyper  "); got != "TalkXTyper" {
		t.Fatalf("sanitizeTranscriptionKeyword() = %q, want TalkXTyper", got)
	}
}
