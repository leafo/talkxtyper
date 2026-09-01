package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	openAILiveTranscriptionModel = "gpt-live-transcribe"
	openAILiveAudioChunkSamples  = openAILiveSampleRate / 10 // 100 ms
	openAIClientSecretURL        = "https://api.openai.com/v1/realtime/client_secrets"
	openAIRealtimeWebSocketURL   = "wss://api.openai.com/v1/realtime"
	liveSetupTimeout             = 15 * time.Second
)

type openAILiveTranscriptionSession struct {
	*liveTextBuffer
	conn      *websocket.Conn
	writeMu   sync.Mutex
	chunker   pcmChunker
	events    chan realtimeServerEvent
	readErr   chan error
	closeOnce sync.Once
}

type realtimeServerEvent struct {
	Type       string `json:"type"`
	ItemID     string `json:"item_id"`
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`
	Session    *struct {
		Type string `json:"type"`
	} `json:"session"`
	Error *struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type realtimeClientSecretRequest struct {
	Session struct {
		Type string `json:"type"`
	} `json:"session"`
}

type realtimeClientSecretResponse struct {
	Value   string `json:"value"`
	Session struct {
		Type string `json:"type"`
	} `json:"session"`
}

type realtimeSessionUpdate struct {
	Type    string                       `json:"type"`
	Session realtimeTranscriptionSession `json:"session"`
}

type realtimeTranscriptionSession struct {
	Type  string                     `json:"type"`
	Audio realtimeTranscriptionAudio `json:"audio"`
}

type realtimeTranscriptionAudio struct {
	Input realtimeTranscriptionAudioInput `json:"input"`
}

type realtimeTranscriptionAudioInput struct {
	Format        realtimePCMFormat             `json:"format"`
	Transcription realtimeTranscriptionSettings `json:"transcription"`
	TurnDetection any                           `json:"turn_detection"`
}

type realtimePCMFormat struct {
	Type string `json:"type"`
	Rate int    `json:"rate"`
}

type realtimeTranscriptionSettings struct {
	Model     string   `json:"model"`
	Prompt    string   `json:"prompt,omitempty"`
	Keywords  []string `json:"keywords,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Delay     string   `json:"delay,omitempty"`
}

type realtimeAudioAppend struct {
	Type  string `json:"type"`
	Audio string `json:"audio"`
}

type realtimeSimpleEvent struct {
	Type string `json:"type"`
}

func newOpenAILiveTranscriptionSession(ctx context.Context, apiKey, clientSecretEndpoint, websocketEndpoint string) (*openAILiveTranscriptionSession, error) {
	// The setup timeout only bounds the handshake steps. readEvents gets the
	// parent ctx because it must outlive setup for the whole session.
	setupCtx, cancel := context.WithTimeout(ctx, liveSetupTimeout)
	defer cancel()

	clientSecret, err := createTranscriptionClientSecret(setupCtx, apiKey, clientSecretEndpoint)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+clientSecret)
	conn, response, err := websocket.Dial(setupCtx, websocketEndpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("connecting to realtime transcription (HTTP %d): %w", response.StatusCode, err)
		}
		return nil, fmt.Errorf("connecting to realtime transcription: %w", err)
	}
	conn.SetReadLimit(1 << 20)

	session := &openAILiveTranscriptionSession{
		liveTextBuffer: newLiveTextBuffer(),
		conn:           conn,
		chunker:        pcmChunker{chunkSamples: openAILiveAudioChunkSamples},
		events:         make(chan realtimeServerEvent, 16),
		readErr:        make(chan error, 1),
	}
	go session.readEvents(ctx)
	if err := session.waitForCreated(setupCtx); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

func createTranscriptionClientSecret(ctx context.Context, apiKey, endpoint string) (string, error) {
	payload := realtimeClientSecretRequest{}
	payload.Session.Type = "transcription"
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding transcription client secret request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating transcription client secret request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("creating transcription client secret: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading transcription client secret response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(responseBody, &apiError) == nil && apiError.Error.Message != "" {
			return "", fmt.Errorf("creating transcription client secret (HTTP %d): %s", response.StatusCode, apiError.Error.Message)
		}
		return "", fmt.Errorf("creating transcription client secret (HTTP %d)", response.StatusCode)
	}

	var secret realtimeClientSecretResponse
	if err := json.Unmarshal(responseBody, &secret); err != nil {
		return "", fmt.Errorf("decoding transcription client secret response: %w", err)
	}
	if secret.Value == "" {
		return "", fmt.Errorf("transcription client secret response contained no value")
	}
	if secret.Session.Type != "transcription" {
		return "", fmt.Errorf("client secret created %q session, want transcription", secret.Session.Type)
	}
	return secret.Value, nil
}

func (s *openAILiveTranscriptionSession) waitForCreated(ctx context.Context) error {
	for {
		event, err := s.nextEvent(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case "session.created":
			if event.Session == nil || event.Session.Type != "transcription" {
				return fmt.Errorf("realtime server created non-transcription session")
			}
			return nil
		case "error":
			return realtimeEventError(event)
		}
	}
}

func (s *openAILiveTranscriptionSession) readEvents(ctx context.Context) {
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, s.conn, &raw); err != nil {
			s.readErr <- err
			return
		}

		var event realtimeServerEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			s.readErr <- fmt.Errorf("decoding realtime event: %w", err)
			return
		}

		// The socket delivers all deltas before the completed event, so once
		// CommitAndWait returns, every delta is already in the buffer.
		if event.Type == "conversation.item.input_audio_transcription.delta" {
			if event.Delta != "" {
				s.appendLiveText(event.Delta)
			}
			continue
		}

		select {
		case s.events <- event:
		case <-ctx.Done():
			return
		}
	}
}

func (s *openAILiveTranscriptionSession) Configure(ctx context.Context, transcriptionContext TranscriptionContext) error {
	ctx, cancel := context.WithTimeout(ctx, liveSetupTimeout)
	defer cancel()

	update := realtimeSessionUpdate{
		Type: "session.update",
		Session: realtimeTranscriptionSession{
			Type: "transcription",
			Audio: realtimeTranscriptionAudio{Input: realtimeTranscriptionAudioInput{
				Format: realtimePCMFormat{Type: "audio/pcm", Rate: openAILiveSampleRate},
				Transcription: realtimeTranscriptionSettings{
					Model:     openAILiveTranscriptionModel,
					Prompt:    transcriptionContext.Prompt,
					Keywords:  transcriptionContext.Keywords,
					Languages: transcriptionContext.Languages,
					Delay:     "low",
				},
				TurnDetection: nil,
			}},
		},
	}
	if err := s.writeJSON(ctx, update); err != nil {
		return err
	}

	for {
		event, err := s.nextEvent(ctx)
		if err != nil {
			return err
		}
		switch event.Type {
		case "session.updated":
			return nil
		case "error":
			return realtimeEventError(event)
		}
	}
}

func (s *openAILiveTranscriptionSession) AppendPCM(ctx context.Context, samples []int16) error {
	return s.chunker.append(samples, func(chunk []int16) error {
		return s.writeAudio(ctx, chunk)
	})
}

func (s *openAILiveTranscriptionSession) CommitAndWait(ctx context.Context) (string, error) {
	if err := s.chunker.flush(func(chunk []int16) error {
		return s.writeAudio(ctx, chunk)
	}); err != nil {
		return "", err
	}
	if err := s.writeJSON(ctx, realtimeSimpleEvent{Type: "input_audio_buffer.commit"}); err != nil {
		return "", err
	}

	committedItemID := ""
	completed := make(map[string]string)
	for {
		event, err := s.nextEvent(ctx)
		if err != nil {
			return "", err
		}
		switch event.Type {
		case "input_audio_buffer.committed":
			committedItemID = event.ItemID
			if transcript, ok := completed[committedItemID]; ok {
				return transcript, nil
			}
		case "conversation.item.input_audio_transcription.completed":
			if event.Transcript == "" {
				return "", fmt.Errorf("live transcription completed without text")
			}
			if committedItemID == "" {
				completed[event.ItemID] = event.Transcript
			} else if event.ItemID == committedItemID {
				return event.Transcript, nil
			}
		case "conversation.item.input_audio_transcription.failed", "error":
			return "", realtimeEventError(event)
		}
	}
}

func (s *openAILiveTranscriptionSession) finalizationReason() string {
	return "transcription completed event"
}

func (s *openAILiveTranscriptionSession) writeAudio(ctx context.Context, samples []int16) error {
	return s.writeJSON(ctx, realtimeAudioAppend{
		Type:  "input_audio_buffer.append",
		Audio: base64.StdEncoding.EncodeToString(pcm16Bytes(samples)),
	})
}

func (s *openAILiveTranscriptionSession) writeJSON(ctx context.Context, value any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := wsjson.Write(ctx, s.conn, value); err != nil {
		return fmt.Errorf("writing realtime event: %w", err)
	}
	return nil
}

func (s *openAILiveTranscriptionSession) nextEvent(ctx context.Context) (realtimeServerEvent, error) {
	select {
	case event := <-s.events:
		return event, nil
	case err := <-s.readErr:
		// A peer may close immediately after sending its final event. The
		// reader queues decoded events before reporting EOF, so prefer any
		// already-buffered event over the close error, requeueing the error
		// so a later call fails fast instead of blocking until ctx expires.
		select {
		case event := <-s.events:
			select {
			case s.readErr <- err:
			default:
			}
			return event, nil
		default:
		}
		return realtimeServerEvent{}, fmt.Errorf("reading realtime event: %w", err)
	case <-ctx.Done():
		return realtimeServerEvent{}, ctx.Err()
	}
}

func (s *openAILiveTranscriptionSession) Close() {
	s.closeOnce.Do(func() {
		_ = s.conn.Close(websocket.StatusNormalClosure, "transcription complete")
	})
}

func realtimeEventError(event realtimeServerEvent) error {
	if event.Error != nil && event.Error.Message != "" {
		return fmt.Errorf("realtime transcription %s: %s", event.Type, event.Error.Message)
	}
	return fmt.Errorf("realtime transcription event: %s", event.Type)
}

func pcm16Bytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	return data
}
