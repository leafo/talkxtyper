package main

import (
	"context"
	"strings"
	"testing"
)

func TestCollectTranscriptionContextManualKeywords(t *testing.T) {
	if got := taskManager.GetContext(); got != "" {
		t.Fatalf("GetContext() before set = %q, want empty", got)
	}

	taskManager.SetContext("watch for request_id and AC-42 today")
	defer taskManager.SetContext("")

	transcriptionContext := collectTranscriptionContext(context.Background())
	if transcriptionContext.Prompt != "" {
		t.Fatalf("manual context must not enter the prompt, got %q", transcriptionContext.Prompt)
	}
	if got := strings.Join(transcriptionContext.Keywords, ","); got != "request_id,AC-42" {
		t.Fatalf("Keywords = %q, want %q", got, "request_id,AC-42")
	}
}
