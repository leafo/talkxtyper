package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestGeminiAPIKeyPrecedence(t *testing.T) {
	previousKey := config.GeminiKey
	config.GeminiKey = "config-key"
	t.Cleanup(func() { config.GeminiKey = previousKey })

	t.Setenv("GEMINI_API_KEY", "gemini-env-key")
	t.Setenv("GOOGLE_API_KEY", "google-env-key")
	if got, err := getGeminiAPIKey(); err != nil || got != "gemini-env-key" {
		t.Fatalf("getGeminiAPIKey() = %q, %v; want Gemini environment key", got, err)
	}

	t.Setenv("GEMINI_API_KEY", "")
	if got, err := getGeminiAPIKey(); err != nil || got != "google-env-key" {
		t.Fatalf("getGeminiAPIKey() = %q, %v; want Google environment key", got, err)
	}

	t.Setenv("GOOGLE_API_KEY", "")
	if got, err := getGeminiAPIKey(); err != nil || got != "config-key" {
		t.Fatalf("getGeminiAPIKey() = %q, %v; want config key", got, err)
	}
}

func TestGeminiTranscriptionConfig(t *testing.T) {
	transcriptionConfig := geminiAudioTranscriptionConfig(TranscriptionContext{
		Languages: []string{"en", "es-419", "auto", ""},
		Keywords:  []string{"request_id", "AC-42"},
	})
	if got := strings.Join(transcriptionConfig.LanguageCodes, ","); got != "en-US,es-419" {
		t.Fatalf("LanguageCodes = %q, want %q", got, "en-US,es-419")
	}
	if got := strings.Join(transcriptionConfig.CustomVocabulary, ","); got != "request_id,AC-42" {
		t.Fatalf("CustomVocabulary = %q, want %q", got, "request_id,AC-42")
	}
	if transcriptionConfig.Mode != genai.AudioTranscriptionConfigModeVerbatim {
		t.Fatalf("Mode = %q, want VERBATIM", transcriptionConfig.Mode)
	}
}

type fakeGeminiLiveSession struct {
	receiveCh chan *genai.LiveServerMessage
	sendCh    chan genai.LiveRealtimeInput
	closeOnce sync.Once
}

func (s *fakeGeminiLiveSession) Receive() (*genai.LiveServerMessage, error) {
	message, ok := <-s.receiveCh
	if !ok {
		return nil, errors.New("closed")
	}
	return message, nil
}

func (s *fakeGeminiLiveSession) SendRealtimeInput(input genai.LiveRealtimeInput) error {
	s.sendCh <- input
	return nil
}

func (s *fakeGeminiLiveSession) Close() error {
	s.closeOnce.Do(func() { close(s.receiveCh) })
	return nil
}

func TestGeminiLiveUsesOnlyFinalizedTranscriptions(t *testing.T) {
	fake := &fakeGeminiLiveSession{
		receiveCh: make(chan *genai.LiveServerMessage, 4),
		sendCh:    make(chan genai.LiveRealtimeInput, 4),
	}
	session := &geminiLiveTranscriptionSession{
		liveDeltaBuffer:   newLiveDeltaBuffer(),
		session:           fake,
		chunker:           pcmChunker{chunkSamples: geminiLiveAudioChunkSamples},
		finalized:         make(chan struct{}, 1),
		activity:          make(chan struct{}, 1),
		readErr:           make(chan error, 1),
		finalizationGrace: 300 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = fake.Close() })
	go session.readEvents()

	// This hypothesis must never be exposed to keyboard injection.
	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InterimInputTranscription: &genai.Transcription{Text: "yellow"},
	}}

	samples := []int16{0x1234, -2}
	if err := session.AppendPCM(ctx, samples); err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := session.CommitAndWait(ctx)
		resultCh <- struct {
			text string
			err  error
		}{text, err}
	}()

	audio := <-fake.sendCh
	if audio.Audio == nil || audio.Audio.MIMEType != "audio/pcm;rate=16000" {
		t.Fatalf("unexpected audio input: %+v", audio)
	}
	if string(audio.Audio.Data) != string(pcm16Bytes(samples)) {
		t.Fatal("Gemini PCM payload does not match input")
	}
	if end := <-fake.sendCh; !end.AudioStreamEnd {
		t.Fatalf("final input = %+v, want AudioStreamEnd", end)
	}

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "hello world"},
	}}
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.text != "hello world" {
		t.Fatalf("transcript = %q, want %q", result.text, "hello world")
	}
	if got := session.takeDeltas(); got != "hello world" {
		t.Fatalf("takeDeltas() = %q, want finalized text only", got)
	}
}

func TestAppendGeminiTranscriptSegmentSpacing(t *testing.T) {
	var transcript strings.Builder
	if got := appendGeminiTranscriptSegment(&transcript, "Hello."); got != "Hello." {
		t.Fatalf("first chunk = %q", got)
	}
	if got := appendGeminiTranscriptSegment(&transcript, "Next sentence"); got != " Next sentence" {
		t.Fatalf("second chunk = %q", got)
	}
	if got := appendGeminiTranscriptSegment(&transcript, "!"); got != "!" {
		t.Fatalf("punctuation chunk = %q", got)
	}
	if got := transcript.String(); got != "Hello. Next sentence!" {
		t.Fatalf("transcript = %q", got)
	}
}

func newFakeGeminiLiveSessionPair(t *testing.T) (*fakeGeminiLiveSession, *geminiLiveTranscriptionSession) {
	t.Helper()
	fake := &fakeGeminiLiveSession{
		receiveCh: make(chan *genai.LiveServerMessage, 4),
		sendCh:    make(chan genai.LiveRealtimeInput, 4),
	}
	session := &geminiLiveTranscriptionSession{
		liveDeltaBuffer:   newLiveDeltaBuffer(),
		session:           fake,
		chunker:           pcmChunker{chunkSamples: geminiLiveAudioChunkSamples},
		finalized:         make(chan struct{}, 1),
		activity:          make(chan struct{}, 1),
		readErr:           make(chan error, 1),
		finalizationGrace: 300 * time.Millisecond,
	}
	t.Cleanup(func() { _ = fake.Close() })
	go session.readEvents()
	return fake, session
}

func TestGeminiLiveCommitWaitsForCurrentTurn(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A pause can finish one turn while recording continues. That event must not
	// cause a later commit to return before the post-pause audio is finalized.
	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "first"},
	}}
	select {
	case <-session.deltaReadyCh():
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcription")
	}

	resultCh := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := session.CommitAndWait(ctx)
		resultCh <- struct {
			text string
			err  error
		}{text, err}
	}()
	if end := <-fake.sendCh; !end.AudioStreamEnd {
		t.Fatalf("final input = %+v, want AudioStreamEnd", end)
	}

	select {
	case result := <-resultCh:
		t.Fatalf("commit returned before current turn completed: %q, %v", result.text, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	// TurnComplete belongs to model-turn processing and is not ordered with the
	// authoritative input transcription.
	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		TurnComplete: true,
	}}
	select {
	case result := <-resultCh:
		t.Fatalf("commit returned on TurnComplete: %q, %v", result.text, result.err)
	case <-time.After(20 * time.Millisecond):
	}

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "second"},
	}}
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.text != "first second" {
		t.Fatalf("transcript = %q, want %q", result.text, "first second")
	}
}

func TestGeminiLiveReturnsPartialTranscriptWithSocketError(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This segment was finalized before the user released the key.
	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "hello world"},
	}}
	select {
	case <-session.deltaReadyCh():
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcription")
	}

	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		text, err := session.CommitAndWait(ctx)
		resultCh <- text
		errCh <- err
	}()
	if end := <-fake.sendCh; !end.AudioStreamEnd {
		t.Fatalf("final input = %+v, want AudioStreamEnd", end)
	}

	_ = fake.Close()

	if err := <-errCh; err == nil {
		t.Fatal("expected socket read error")
	}
	if text := <-resultCh; text != "hello world" {
		t.Fatalf("transcript = %q, want %q", text, "hello world")
	}
}

func TestGeminiLiveUsesTranscriptFinalizedBeforeStop(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "already final"},
	}}
	select {
	case <-session.deltaReadyCh():
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcription")
	}

	resultCh := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := session.CommitAndWait(ctx)
		resultCh <- struct {
			text string
			err  error
		}{text, err}
	}()
	if end := <-fake.sendCh; !end.AudioStreamEnd {
		t.Fatalf("final input = %+v, want AudioStreamEnd", end)
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.text != "already final" {
		t.Fatalf("transcript = %q, want %q", result.text, "already final")
	}
}

// commitGeminiSession runs CommitAndWait in the background and consumes the
// AudioStreamEnd the session sends on the way in.
func commitGeminiSession(ctx context.Context, t *testing.T, fake *fakeGeminiLiveSession, session *geminiLiveTranscriptionSession) <-chan struct {
	text string
	err  error
} {
	t.Helper()
	resultCh := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := session.CommitAndWait(ctx)
		resultCh <- struct {
			text string
			err  error
		}{text, err}
	}()
	if end := <-fake.sendCh; !end.AudioStreamEnd {
		t.Errorf("final input = %+v, want AudioStreamEnd", end)
	}
	return resultCh
}

// A transcription message that carries no text (a bare end-of-transcription
// marker for speech finalized before the user released the key) must not end
// the commit, or the tail of the recording is dropped.
func TestGeminiLiveIgnoresTextlessTranscription(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "before the pause"},
	}}
	select {
	case <-session.deltaReadyCh():
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcription")
	}

	resultCh := commitGeminiSession(ctx, t, fake, session)

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{},
	}}
	select {
	case result := <-resultCh:
		t.Fatalf("commit returned on a text-less transcription: %q, %v", result.text, result.err)
	case <-time.After(100 * time.Millisecond):
	}

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "and the tail", Finished: true},
	}}
	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.text != "before the pause and the tail" {
		t.Fatalf("transcript = %q, want the tail included", result.text)
	}
}

// A transcription split across several messages must be collected in full:
// each piece restarts the grace instead of the first one ending the wait.
func TestGeminiLiveCollectsMultiMessageTail(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "one"},
	}}
	select {
	case <-session.deltaReadyCh():
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcription")
	}

	resultCh := commitGeminiSession(ctx, t, fake, session)

	// Spaced closer together than the grace, but well past it in total.
	for _, text := range []string{"two", "three", "four"} {
		time.Sleep(session.finalizationGrace / 2)
		select {
		case result := <-resultCh:
			t.Fatalf("commit returned mid-transcription: %q, %v", result.text, result.err)
		default:
		}
		fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
			InputTranscription: &genai.Transcription{Text: text},
		}}
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.text != "one two three four" {
		t.Fatalf("transcript = %q, want every message", result.text)
	}
}

// Finished is authoritative: the commit returns on it without waiting out the
// grace.
func TestGeminiLiveReturnsImmediatelyOnFinished(t *testing.T) {
	fake, session := newFakeGeminiLiveSessionPair(t)
	session.finalizationGrace = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resultCh := commitGeminiSession(ctx, t, fake, session)

	start := time.Now()
	fake.receiveCh <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "all done", Finished: true},
	}}
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.text != "all done" {
			t.Fatalf("transcript = %q, want %q", result.text, "all done")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("commit did not return on Finished after %s", time.Since(start))
	}
}
