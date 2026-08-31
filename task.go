package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type TranscriptionResult struct {
	UUID                  string
	Original              string
	Modified              string
	TranscriptionProvider TranscriptionProvider
	TranscriptionMode     TranscriptionMode
	TranscriptionModel    string
	TranscriptionElapsed  time.Duration
	TranscriptionKeywords []string
	RepairPrompt          string
	RepairModel           string
	RepairElapsed         time.Duration
	Mp3Recording          []byte `json:"-"`
}

func NewTranscriptionResult() *TranscriptionResult {
	return &TranscriptionResult{
		UUID: uuid.New().String(),
	}
}

func (tr *TranscriptionResult) String() string {
	if tr.Modified != "" {
		return tr.Modified
	}
	return tr.Original
}

// NOTE: all methods for this type should be thread safe
type TranscribeTask struct {
	stopRecordingCh   chan struct{}
	waitForCompletion chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	result            *TranscriptionResult
	provider          TranscriptionProvider
	mode              TranscriptionMode
	state             atomic.Int32
	mu                sync.Mutex
}

// TODO: this should take a context
func NewTranscribeTask(provider TranscriptionProvider, mode TranscriptionMode) *TranscribeTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &TranscribeTask{
		ctx:      ctx,
		cancel:   cancel,
		provider: normalizeTranscriptionProvider(provider),
		mode:     normalizeTranscriptionMode(mode),
	}
}

// stop the recording so that transcription can be started
func (t *TranscribeTask) StopRecording() {
	if TaskState(t.state.Load()) == TaskStateIdle {
		t.cancel()
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopRecordingCh != nil {
		close(t.stopRecordingCh)
		t.stopRecordingCh = nil
	}
}

// cancel the task, regardless of state
func (t *TranscribeTask) Abort() {
	t.cancel()
}

func (t *TranscribeTask) GetResult() *TranscriptionResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result
}

func (t *TranscribeTask) SetResult(result *TranscriptionResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = result
}

func (t *TranscribeTask) emitState(stateCh chan<- TaskState, state TaskState) {
	t.state.Store(int32(state))
	stateCh <- state
}

// TODO: this is designed to only be called once, but consider thread safety
func (t *TranscribeTask) Start() chan TaskState {
	t.stopRecordingCh = make(chan struct{})
	t.waitForCompletion = make(chan struct{})
	stateCh := make(chan TaskState)

	go func() {
		defer close(t.waitForCompletion)
		defer close(stateCh)
		defer t.state.Store(int32(TaskStateIdle))

		var err error
		if t.mode == TranscriptionModeLive {
			err = t.runLive(stateCh)
		} else {
			err = t.runBuffered(stateCh)
		}
		if err != nil && t.ctx.Err() == nil {
			notifyError("Transcription failed", err)
		}
	}()

	return stateCh
}

func (t *TranscribeTask) runBuffered(stateCh chan<- TaskState) error {
	t.emitState(stateCh, TaskStateRecording)
	contextCh := collectTranscriptionContextAsync(t.ctx)

	recording, err := recordAudioStream(t.ctx, t.stopRecordingCh, sampleRate, nil)
	if err != nil {
		return err
	}
	mp3Path, err := writeAudioRecordingToMP3(recording)
	if err != nil {
		return fmt.Errorf("writing MP3 file: %w", err)
	}
	defer os.Remove(mp3Path)

	t.emitState(stateCh, TaskStateTranscribing)
	log.Println("Audio ready, waiting for description")
	transcriptionContext := <-contextCh
	log.Printf("Transcription keywords: %q", transcriptionContext.Keywords)

	result, err := transcribeAudio(t.ctx, t.provider, mp3Path, transcriptionContext)
	if err != nil {
		return err
	}
	attachMP3(result, mp3Path)
	t.complete(result)
	return nil
}

func (t *TranscribeTask) runLive(stateCh chan<- TaskState) error {
	contextCh := collectTranscriptionContextAsync(t.ctx)

	// Recording starts immediately while context collection and session setup
	// happen in the background. Audio buffers locally until the provider is
	// configured, then flushes and streams from there.
	type preparedSession struct {
		session              liveTranscriptionSessionAPI
		transcriptionContext TranscriptionContext
		model                string
		err                  error
	}
	sessionCh := make(chan preparedSession, 1)
	typerSessionCh := make(chan liveTranscriptionSessionAPI, 1)
	go func() {
		var prepared preparedSession
		prepared.err = func() error {
			session, model, transcriptionContext, err := newConfiguredLiveTranscriptionSession(t.ctx, t.provider, contextCh)
			prepared.transcriptionContext = transcriptionContext
			if err != nil {
				return err
			}
			log.Printf("Live transcription keywords: %q", prepared.transcriptionContext.Keywords)
			prepared.session = session
			prepared.model = model
			return nil
		}()
		if prepared.session != nil {
			typerSessionCh <- prepared.session
		} else {
			close(typerSessionCh)
		}
		sessionCh <- prepared
	}()

	var session liveTranscriptionSessionAPI
	var transcriptionContext TranscriptionContext
	var transcriptionModel string
	sessionTaken := false
	defer func() {
		if session != nil {
			session.Close()
		} else if !sessionTaken {
			// setup may still be in flight; close whatever it produces
			go func() {
				if prepared := <-sessionCh; prepared.session != nil {
					prepared.session.Close()
				}
			}()
		}
	}()

	// Deltas are typed the moment they arrive; there is no repair pass since
	// typed text cannot be revised. An abort must stop typing immediately and
	// discard anything still buffered.
	stopCh := make(chan bool)
	typedCh := make(chan string, 1)
	go func() {
		var typed strings.Builder
		var typerSession liveTranscriptionSessionAPI
		select {
		case typerSession = <-typerSessionCh:
		case <-stopCh:
			typedCh <- ""
			return
		}
		if typerSession == nil {
			<-stopCh
			typedCh <- ""
			return
		}
		typePending := func() {
			if t.ctx.Err() != nil {
				return
			}
			if chunk := typerSession.takeDeltas(); chunk != "" {
				typeString(chunk)
				typed.WriteString(chunk)
			}
		}
		for {
			select {
			case <-typerSession.deltaReadyCh():
				typePending()
			case drain := <-stopCh:
				if drain {
					typePending()
				}
				typedCh <- typed.String()
				return
			}
		}
	}()
	typerStopped := false
	stopTyper := func(drain bool) string {
		if typerStopped {
			return ""
		}
		typerStopped = true
		stopCh <- drain
		return <-typedCh
	}
	defer stopTyper(false)

	t.emitState(stateCh, TaskStateRecording)
	var buffered []int16
	recording, err := recordAudioStream(t.ctx, t.stopRecordingCh, liveSampleRate(t.provider), func(chunk []int16) error {
		if session == nil {
			select {
			case prepared := <-sessionCh:
				sessionTaken = true
				if prepared.err != nil {
					return prepared.err
				}
				session = prepared.session
				transcriptionContext = prepared.transcriptionContext
				transcriptionModel = prepared.model
				if err := session.AppendPCM(t.ctx, buffered); err != nil {
					return err
				}
				buffered = nil
			default:
				buffered = append(buffered, chunk...)
				return nil
			}
		}
		return session.AppendPCM(t.ctx, chunk)
	})
	if err != nil {
		return err
	}

	t.emitState(stateCh, TaskStateFinalizing)
	// Recording ended before the session came up: wait for it, then send the
	// whole recording at once.
	if session == nil {
		prepared := <-sessionCh
		sessionTaken = true
		if prepared.err != nil {
			return prepared.err
		}
		session = prepared.session
		transcriptionContext = prepared.transcriptionContext
		transcriptionModel = prepared.model
		if err := session.AppendPCM(t.ctx, recording.Samples); err != nil {
			return err
		}
	}

	finalizeCtx, cancel := context.WithTimeout(t.ctx, 20*time.Second)
	defer cancel()
	transcript, err := session.CommitAndWait(finalizeCtx)
	if err != nil {
		return fmt.Errorf("finalizing live transcription: %w", err)
	}

	typed := stopTyper(true)
	if remainder, ok := strings.CutPrefix(transcript, typed); ok {
		if remainder != "" {
			typeString(remainder)
		}
	} else {
		log.Printf("Live transcript diverged from typed deltas, leaving typed text as-is.\ntyped: %q\nfinal: %q", typed, transcript)
	}

	result := NewTranscriptionResult()
	result.Original = transcript
	result.TranscriptionProvider = t.provider
	result.TranscriptionMode = TranscriptionModeLive
	result.TranscriptionModel = transcriptionModel
	result.TranscriptionKeywords = transcriptionContext.Keywords
	mp3Path, err := writeAudioRecordingToMP3(recording)
	if err != nil {
		log.Printf("Error writing live MP3 history: %v", err)
	} else {
		defer os.Remove(mp3Path)
		attachMP3(result, mp3Path)
	}
	t.complete(result)
	return nil
}

func collectTranscriptionContextAsync(ctx context.Context) <-chan TranscriptionContext {
	contextCh := make(chan TranscriptionContext, 1)
	go func() {
		defer close(contextCh)
		contextCh <- collectTranscriptionContext(ctx)
	}()
	return contextCh
}

func collectTranscriptionContext(ctx context.Context) TranscriptionContext {
	prompt := collectContextPrompt(ctx)
	transcriptionContext := NewTranscriptionContext(prompt)

	// The manually set context (HTTP /context page) only contributes keyword
	// hints; it does not enter the prompt.
	if manual := taskManager.GetContext(); manual != "" {
		transcriptionContext.Keywords = extractTranscriptionKeywords(prompt + "\n" + manual)
	}
	return transcriptionContext
}

func collectContextPrompt(ctx context.Context) string {
	if config.IncludeScreen {
		description, err := describeScreen(ctx)
		if err != nil {
			// Transcription still runs, just without screen context, so the
			// failure would otherwise be invisible.
			notifyError("Could not read screen context", err)
			return ""
		}
		log.Printf("Screen Description: %s\n", description)
		return description + "\nPlease use the information about the user's screen to aid in transcribing the audio"
	}

	if config.IncludeNvim {
		nvimClient := NewNvimClient()
		if err := nvimClient.FindActiveNvim(); err != nil {
			log.Printf("nvim: %v", err)
		} else {
			log.Printf("Using nvim socket: %s", nvimClient.socketFile)
			context, err := nvimClient.BuildTranscriptionContext()
			if err != nil {
				log.Printf("nvim context: %v", err)
			} else {
				log.Printf("nvim context: %s", context)
				return context
			}
		}
	}

	if config.IncludeTmux {
		context, err := BuildTmuxTranscriptionContext()
		if err != nil {
			log.Printf("tmux: %v", err)
			return ""
		}
		log.Printf("tmux context: %s", context)
		return context
	}
	return ""
}

func attachMP3(result *TranscriptionResult, mp3Path string) {
	mp3Data, err := os.ReadFile(mp3Path)
	log.Printf("MP3 data size: %d bytes, MP3 path: %s, error: %v\n", len(mp3Data), mp3Path, err)
	if err == nil {
		result.Mp3Recording = mp3Data
	}
}

func (t *TranscribeTask) complete(result *TranscriptionResult) {
	if transcriptionJSON, err := json.Marshal(result); err == nil {
		log.Printf("Transcription: %s\n", transcriptionJSON)
	}
	t.SetResult(result)
	taskManager.AppendToHistory(result)
}
