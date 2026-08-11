package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestRealtimeEndpointsCreateTranscriptionSession(t *testing.T) {
	parsed, err := url.Parse(openAIRealtimeWebSocketURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("model"); got != "" {
		t.Fatalf("realtime URL must not select a realtime session model, got %q", got)
	}
	secretURL, err := url.Parse(openAIClientSecretURL)
	if err != nil {
		t.Fatal(err)
	}
	if secretURL.Path != "/v1/realtime/client_secrets" {
		t.Fatalf("client secret path = %q", secretURL.Path)
	}
}

func TestLiveTranscriptionSession(t *testing.T) {
	expectedContext := TranscriptionContext{
		Prompt:    "Editing request_id AC-42",
		Keywords:  []string{"request_id", "AC-42"},
		Languages: []string{"en"},
	}
	samples := make([]int16, liveAudioChunkSamples+2)
	samples[0] = 0x1234
	samples[len(samples)-2] = -2
	samples[len(samples)-1] = 0x0102

	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/realtime/client_secrets" {
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				serverErr <- fmt.Errorf("client secret Authorization = %q", got)
				return
			}
			var request realtimeClientSecretRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				serverErr <- err
				return
			}
			if request.Session.Type != "transcription" {
				serverErr <- fmt.Errorf("client secret session type = %q", request.Session.Type)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value":   "test-ephemeral-key",
				"session": map[string]any{"type": "transcription"},
			})
			return
		}
		if r.URL.Path != "/v1/realtime" {
			serverErr <- fmt.Errorf("unexpected path %q", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-ephemeral-key" {
			serverErr <- fmt.Errorf("websocket Authorization = %q", got)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()

		ctx := r.Context()
		if err := wsjson.Write(ctx, conn, map[string]any{
			"type":    "session.created",
			"session": map[string]any{"type": "transcription"},
		}); err != nil {
			serverErr <- err
			return
		}
		var update realtimeSessionUpdate
		if err := wsjson.Read(ctx, conn, &update); err != nil {
			serverErr <- err
			return
		}
		settings := update.Session.Audio.Input.Transcription
		if update.Type != "session.update" || update.Session.Type != "transcription" {
			serverErr <- fmt.Errorf("unexpected session update: %+v", update)
			return
		}
		if update.Session.Audio.Input.Format != (realtimePCMFormat{Type: "audio/pcm", Rate: liveSampleRate}) {
			serverErr <- fmt.Errorf("unexpected audio format: %+v", update.Session.Audio.Input.Format)
			return
		}
		if settings.Model != liveTranscriptionModel || settings.Prompt != expectedContext.Prompt || settings.Delay != "low" {
			serverErr <- fmt.Errorf("unexpected transcription settings: %+v", settings)
			return
		}
		if strings.Join(settings.Keywords, ",") != "request_id,AC-42" || strings.Join(settings.Languages, ",") != "en" {
			serverErr <- fmt.Errorf("unexpected hints: %+v", settings)
			return
		}
		if update.Session.Audio.Input.TurnDetection != nil {
			serverErr <- fmt.Errorf("turn detection should be null: %#v", update.Session.Audio.Input.TurnDetection)
			return
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "session.updated"}); err != nil {
			serverErr <- err
			return
		}

		var received []byte
		for {
			var raw json.RawMessage
			if err := wsjson.Read(ctx, conn, &raw); err != nil {
				serverErr <- err
				return
			}
			var event struct {
				Type  string `json:"type"`
				Audio string `json:"audio"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				serverErr <- err
				return
			}
			switch event.Type {
			case "input_audio_buffer.append":
				data, err := base64.StdEncoding.DecodeString(event.Audio)
				if err != nil {
					serverErr <- err
					return
				}
				received = append(received, data...)
			case "input_audio_buffer.commit":
				if string(received) != string(pcm16Bytes(samples)) {
					serverErr <- fmt.Errorf("received PCM does not match input")
					return
				}
				// Completion order across items is not guaranteed. Exercise the
				// client's item-ID correlation by completing before commit ack.
				_ = wsjson.Write(ctx, conn, map[string]any{
					"type":    "conversation.item.input_audio_transcription.delta",
					"item_id": "item-1", "delta": "hello",
				})
				_ = wsjson.Write(ctx, conn, map[string]any{
					"type":    "conversation.item.input_audio_transcription.completed",
					"item_id": "item-1", "transcript": "hello world",
				})
				_ = wsjson.Write(ctx, conn, map[string]any{
					"type": "input_audio_buffer.committed", "item_id": "item-1",
				})
				serverErr <- nil
				return
			default:
				serverErr <- fmt.Errorf("unexpected client event %q", event.Type)
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientSecretEndpoint := server.URL + "/v1/realtime/client_secrets"
	websocketEndpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/realtime"
	session, err := newLiveTranscriptionSession(ctx, "test-key", clientSecretEndpoint, websocketEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Configure(ctx, expectedContext); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendPCM(ctx, samples); err != nil {
		t.Fatal(err)
	}
	transcript, err := session.CommitAndWait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if transcript != "hello world" {
		t.Fatalf("transcript = %q, want %q", transcript, "hello world")
	}
	// The delta preceded the completed event on the socket, so it must
	// already be buffered by the time CommitAndWait returns.
	if got := session.takeDeltas(); got != "hello" {
		t.Fatalf("takeDeltas() = %q, want %q", got, "hello")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestPCM16BytesAreLittleEndian(t *testing.T) {
	got := pcm16Bytes([]int16{0x1234, -2})
	want := []byte{0x34, 0x12, 0xfe, 0xff}
	if string(got) != string(want) {
		t.Fatalf("pcm16Bytes() = %v, want %v", got, want)
	}
}
