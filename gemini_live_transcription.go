package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/genai"
)

const (
	geminiLiveTranscriptionModel = "gemini-3.5-transcribe-live"
	geminiLiveSampleRate         = 16000
	geminiLiveAudioChunkSamples  = geminiLiveSampleRate / 10
	geminiFinalizationGrace      = 1500 * time.Millisecond
	geminiFinalizationTimeout    = 5 * time.Second
)

type geminiLiveTranscriptionSession struct {
	*liveDeltaBuffer
	session   geminiLiveSessionAPI
	chunker   pcmChunker
	closeOnce sync.Once

	transcriptMu sync.Mutex
	transcript   strings.Builder
	finalized    chan struct{}
	readErr      chan error

	// Tests shorten this; production sessions use geminiFinalizationGrace.
	finalizationGrace time.Duration
}

type geminiLiveSessionAPI interface {
	Receive() (*genai.LiveServerMessage, error)
	SendRealtimeInput(genai.LiveRealtimeInput) error
	Close() error
}

func newGeminiLiveTranscriptionSession(ctx context.Context, transcriptionContext TranscriptionContext) (*geminiLiveTranscriptionSession, error) {
	client, err := getGeminiClient(ctx)
	if err != nil {
		return nil, err
	}
	setupCtx, cancel := context.WithTimeout(ctx, liveSetupTimeout)
	defer cancel()
	type connectResult struct {
		session *genai.Session
		err     error
	}
	connectCh := make(chan connectResult, 1)
	go func() {
		session, err := client.Live.Connect(setupCtx, geminiLiveTranscriptionModel, &genai.LiveConnectConfig{
			ResponseModalities:      []genai.Modality{genai.ModalityText},
			InputAudioTranscription: geminiAudioTranscriptionConfig(transcriptionContext),
		})
		connectCh <- connectResult{session: session, err: err}
	}()

	var session *genai.Session
	select {
	case connected := <-connectCh:
		if connected.err != nil {
			return nil, fmt.Errorf("connecting to Gemini live transcription: %w", connected.err)
		}
		session = connected.session
	case <-setupCtx.Done():
		// The SDK's WebSocket dial does not currently accept a context. Arrange
		// cleanup if its own handshake timeout completes after ours.
		go func() {
			if connected := <-connectCh; connected.session != nil {
				_ = connected.session.Close()
			}
		}()
		return nil, fmt.Errorf("connecting to Gemini live transcription: %w", setupCtx.Err())
	}

	result := &geminiLiveTranscriptionSession{
		liveDeltaBuffer: newLiveDeltaBuffer(),
		session:         session,
		chunker:         pcmChunker{chunkSamples: geminiLiveAudioChunkSamples},
		finalized:       make(chan struct{}, 1),
		readErr:         make(chan error, 1),
	}
	go result.readEvents()
	return result, nil
}

func (s *geminiLiveTranscriptionSession) readEvents() {
	for {
		message, err := s.session.Receive()
		if err != nil {
			select {
			case s.readErr <- err:
			default:
			}
			return
		}
		content := message.ServerContent
		if content == nil {
			continue
		}

		// InterimInputTranscription is deliberately ignored: Gemini documents it
		// as a revisable hypothesis, which is unsafe to inject into arbitrary apps.
		chunk := ""
		if final := content.InputTranscription; final != nil {
			if final.Text != "" {
				s.transcriptMu.Lock()
				chunk = appendGeminiTranscriptSegment(&s.transcript, final.Text)
				s.transcriptMu.Unlock()
			}
			// For Gemini 3.5 Transcribe Live, InputTranscription is itself the
			// finalized, authoritative segment. It does not need Finished or
			// TurnComplete, whose ordering is independent of transcription.
			s.signalFinalized()
		}
		if chunk != "" {
			s.queueDelta(chunk)
		}
	}
}

func appendGeminiTranscriptSegment(transcript *strings.Builder, text string) string {
	if transcript.Len() == 0 || text == "" {
		transcript.WriteString(text)
		return text
	}
	current := transcript.String()
	last, _ := utf8.DecodeLastRuneInString(current)
	first, _ := utf8.DecodeRuneInString(text)
	separator := ""
	if !unicode.IsSpace(last) && !unicode.IsSpace(first) && !strings.ContainsRune(".,!?;:)]}", first) {
		separator = " "
	}
	chunk := separator + text
	transcript.WriteString(chunk)
	return chunk
}

func (s *geminiLiveTranscriptionSession) signalFinalized() {
	select {
	case s.finalized <- struct{}{}:
	default:
	}
}

func (s *geminiLiveTranscriptionSession) AppendPCM(_ context.Context, samples []int16) error {
	return s.chunker.append(samples, s.writeAudio)
}

func (s *geminiLiveTranscriptionSession) CommitAndWait(ctx context.Context) (string, error) {
	if err := s.chunker.flush(s.writeAudio); err != nil {
		return "", err
	}
	previousTranscript := s.transcriptText()
	// Discard notifications for segments finalized at pauses earlier in this
	// recording. AudioStreamEnd normally prompts a fresh finalized segment.
	for {
		select {
		case <-s.finalized:
			continue
		default:
			goto drained
		}
	}

drained:
	if err := s.session.SendRealtimeInput(genai.LiveRealtimeInput{AudioStreamEnd: true}); err != nil {
		return "", fmt.Errorf("ending Gemini live audio stream: %w", err)
	}

	// If the server had already finalized all speech during a pause, it may have
	// no new text to emit for AudioStreamEnd. Give a fresh segment a short chance
	// to arrive, then accept the authoritative transcript already received.
	var graceTimer *time.Timer
	var graceCh <-chan time.Time
	if previousTranscript != "" {
		grace := s.finalizationGrace
		if grace <= 0 {
			grace = geminiFinalizationGrace
		}
		graceTimer = time.NewTimer(grace)
		graceCh = graceTimer.C
		defer graceTimer.Stop()
	}
	timeoutTimer := time.NewTimer(geminiFinalizationTimeout)
	defer timeoutTimer.Stop()

	select {
	case <-s.finalized:
		return s.completedTranscript()
	case <-graceCh:
		return s.completedTranscript()
	case err := <-s.readErr:
		return s.transcriptText(), fmt.Errorf("reading Gemini live transcription: %w", err)
	case <-timeoutTimer.C:
		return s.transcriptText(), fmt.Errorf("timed out waiting for Gemini live finalization")
	case <-ctx.Done():
		return s.transcriptText(), ctx.Err()
	}
}

func (s *geminiLiveTranscriptionSession) transcriptText() string {
	s.transcriptMu.Lock()
	defer s.transcriptMu.Unlock()
	return s.transcript.String()
}

func (s *geminiLiveTranscriptionSession) completedTranscript() (string, error) {
	transcript := s.transcriptText()
	if transcript == "" {
		return "", fmt.Errorf("Gemini live transcription completed without text")
	}
	return transcript, nil
}

func (s *geminiLiveTranscriptionSession) writeAudio(samples []int16) error {
	if err := s.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			Data:     pcm16Bytes(samples),
			MIMEType: fmt.Sprintf("audio/pcm;rate=%d", geminiLiveSampleRate),
		},
	}); err != nil {
		return fmt.Errorf("streaming audio to Gemini: %w", err)
	}
	return nil
}

func (s *geminiLiveTranscriptionSession) Close() {
	s.closeOnce.Do(func() { _ = s.session.Close() })
}
