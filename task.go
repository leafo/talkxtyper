package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/google/uuid"
)

type TranscriptionResult struct {
	UUID         string
	Original     string
	Modified     string
	RepairPrompt string
	Mp3Recording []byte `json:"-"`
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
	mu                sync.Mutex
}

// TODO: this should take a context
func NewTranscribeTask() *TranscribeTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &TranscribeTask{
		ctx:    ctx,
		cancel: cancel,
	}
}

// stop the recording so that transcription can be started
func (t *TranscribeTask) StopRecording() {
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

// TODO: this is designed to only be called once, but consider thread safety
func (t *TranscribeTask) Start() chan TaskState {
	t.stopRecordingCh = make(chan struct{})
	t.waitForCompletion = make(chan struct{})
	stateCh := make(chan TaskState)

	go func() {
		defer close(t.waitForCompletion)
		defer close(stateCh)

		stateCh <- TaskStateRecording

		descriptionCh := make(chan string, 1)

		if config.IncludeScreen {
			go func() {
				defer close(descriptionCh)
				description, err := describeScreen(t.ctx)
				if err != nil {
					log.Printf("Error describing screen: %v\n", err)
					return
				}
				log.Printf("Screen Description: %s\n", description)

				description = fmt.Sprintf(description, "\nPlease use the information about the user's screen to aid to transcribing the audio")
				descriptionCh <- description
			}()
		} else if config.IncludeNvim {
			go func() {
				defer close(descriptionCh)
				nvimClient := NewNvimClient()
				if err := nvimClient.FindActiveNvim(); err != nil {
					log.Printf("nivm: %v", err)
					return
				}

				log.Printf("Using nvim socket: %s", nvimClient.socketFile)

				var visibleText string
				var err error

				currentMode, err := nvimClient.GetCurrentMode()
				if err != nil {
					log.Printf("Error getting current nvim mode: %v", err)
					return
				}

				switch currentMode {
				case InsertMode:
					insertionText, err := nvimClient.GetInsertionText("{{CURSOR}}")

					if err != nil {
						log.Printf("Error getting insertion text: %v", err)
						return
					}

					log.Printf("Inserting nvim context: %s", insertionText)
					descriptionCh <- fmt.Sprintf(
						"The user is inserting into a text editor with the following content. The cursor is located at {{CURSOR}}:\n%s",
						insertionText,
					)

				case NormalMode, VisualMode, CommandMode:
					visibleText, err = nvimClient.GetVisibleText()
					if err != nil {
						log.Printf("Error getting visible text: %v", err)
						return
					}

					log.Printf("Visible nvim context: %s", visibleText)
					descriptionCh <- fmt.Sprintf(
						"The user is in a text editor with the following content:\n%s",
						visibleText,
					)

				default:
					log.Printf("Unhandled nvim mode, skipping description: %s", currentMode)
					return
				}

			}()
		} else {
			close(descriptionCh)
		}

		recordingBuffer, err := recordAudio(t.ctx, t.stopRecordingCh)
		if err != nil {
			log.Printf("%v\n", err)
			return
		}

		mp3Path, err := writeRecordingToMP3(recordingBuffer)
		if err != nil {
			log.Printf("Error writing MP3 file: %v\n", err)
			return
		}
		defer os.Remove(mp3Path)

		stateCh <- TaskStateTranscribing

		log.Println("Audio ready, waiting for description")
		var description string
		for d := range descriptionCh {
			description = d
		}

		transcription, err := transcribeAudio(t.ctx, mp3Path, description)

		if err != nil {
			log.Printf("Error transcribing audio: %v\n", err)
			return
		}

		mp3Data, err := os.ReadFile(mp3Path)
		log.Printf("MP3 data size: %d bytes, MP3 path: %s, error: %v\n", len(mp3Data), mp3Path, err)

		if err == nil {
			transcription.Mp3Recording = mp3Data
		}

		transcriptionJSON, err := json.Marshal(transcription)
		if err == nil {
			log.Printf("Transcription: %s\n", transcriptionJSON)
		}

		t.SetResult(transcription)
		taskManager.AppendToHistory(transcription)
	}()

	return stateCh
}
