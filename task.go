package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

type TaskState int

const (
	TaskStateIdle TaskState = iota
	TaskStateRecording
	TaskStateTranscribing
)

type TaskManager struct {
	currentTask      atomic.Pointer[TranscribeTask]
	transcriptionRes chan TranscriptionResult
	stateCh          chan TaskState
	context          atomic.Pointer[string]
	history          atomic.Pointer[[]TranscriptionResult]
}

type TranscriptionResult struct {
	Original     string
	Modified     string
	RepairPrompt string
}

func (tr *TranscriptionResult) String() string {
	if tr.Modified != "" {
		return tr.Modified
	}
	return tr.Original
}

func (tr *TranscriptionResult) IsEmpty() bool {
	return tr.Original == "" && tr.Modified == ""
}

// task managers ensures only only one task is running at a time and cancels
// the current task if a new one is started
var taskManager = TaskManager{
	currentTask:      atomic.Pointer[TranscribeTask]{}, // Initialize as nil
	transcriptionRes: make(chan TranscriptionResult),
	stateCh:          make(chan TaskState, 10),
	context:          atomic.Pointer[string]{},
	history:          atomic.Pointer[[]TranscriptionResult]{},
}

func (tm *TaskManager) StartNewTask() {
	newTask := NewTranscribeTask()

	oldTask := tm.currentTask.Swap(newTask)
	if oldTask != nil {
		oldTask.Abort()
	}

	stateCh := newTask.Start()

	go func() {
		for state := range stateCh {
			tm.stateCh <- state
		}

		tm.stateCh <- TaskStateIdle
		tm.currentTask.CompareAndSwap(newTask, nil)

		if result := newTask.GetResult(); !result.IsEmpty() {
			tm.transcriptionRes <- result
		}
	}()
}

func (tm *TaskManager) StartOrStopTask() {
	if currentTask := tm.currentTask.Load(); currentTask != nil {
		tm.StopRecording()
	} else {
		tm.StartNewTask()
	}
}

func (tm *TaskManager) StopRecording() {
	if currentTask := tm.currentTask.Load(); currentTask != nil {
		currentTask.StopRecording()
	}
}

func (tm *TaskManager) Abort() {
	if currentTask := tm.currentTask.Load(); currentTask != nil {
		currentTask.Abort()
	}
}

type TranscribeTask struct {
	stopRecordingCh chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	result          TranscriptionResult
	mu              sync.Mutex
}

func NewTranscribeTask() *TranscribeTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &TranscribeTask{
		ctx:    ctx,
		cancel: cancel,
		result: TranscriptionResult{},
	}
}

// stop the recording so that transcription can be started
func (t *TranscribeTask) StopRecording() {
	if t.stopRecordingCh != nil {
		close(t.stopRecordingCh)
		t.stopRecordingCh = nil
	}
}

// cancel the task, regardless of state
func (t *TranscribeTask) Abort() {
	t.cancel()
}

func (t *TranscribeTask) GetResult() TranscriptionResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result
}

func (t *TranscribeTask) SetResult(result TranscriptionResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = result
}

func (t *TranscribeTask) Start() chan TaskState {
	t.stopRecordingCh = make(chan struct{})
	stateCh := make(chan TaskState)

	go func() {
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
			close(stateCh)
			return
		}

		mp3FileName, err := writeRecordingToMP3(recordingBuffer)
		if err != nil {
			log.Printf("Error writing MP3 file: %v\n", err)
			close(stateCh)
			return
		}
		defer os.Remove(mp3FileName)

		stateCh <- TaskStateTranscribing

		log.Println("Audio ready, waiting for description")
		var description string
		for d := range descriptionCh {
			description = d
		}

		transcription, err := transcribeAudio(t.ctx, mp3FileName, description)
		if err != nil {
			log.Printf("Error transcribing audio: %v\n", err)
			close(stateCh)
			return
		}

		transcriptionJSON, err := json.Marshal(transcription)
		if err == nil {
			log.Printf("Transcription: %s\n", transcriptionJSON)
		}

		t.SetResult(transcription)
		taskManager.AppendToHistory(transcription)
		close(stateCh)
	}()

	return stateCh
}

func (tm *TaskManager) GetContext() string {
	return *tm.context.Load()
}

func (tm *TaskManager) SetContext(ctx string) {
	tm.context.Store(&ctx)
}

func (tm *TaskManager) AppendToHistory(entry TranscriptionResult) {
	for {
		oldHistory := tm.history.Load()
		if oldHistory == nil {
			newHistory := []TranscriptionResult{entry}
			if tm.history.CompareAndSwap(nil, &newHistory) {
				break
			}
		} else {
			newHistory := append(*oldHistory, entry)
			if tm.history.CompareAndSwap(oldHistory, &newHistory) {
				break
			}
		}
	}
}

func (tm *TaskManager) GetHistory() []TranscriptionResult {
	history := tm.history.Load()
	if history == nil {
		return []TranscriptionResult{}
	}
	return append([]TranscriptionResult(nil), *history...)
}
