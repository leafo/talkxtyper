package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	context          atomic.Value // string
	history          []string     // history of transcriptions
}

// task managers ensures only only one task is running at a time and cancels
// the current task if a new one is started
var taskManager = TaskManager{
	currentTask:      atomic.Pointer[TranscribeTask]{}, // Initialize as nil
	transcriptionRes: make(chan TranscriptionResult),
	stateCh:          make(chan TaskState, 10),
}

func (tm *TaskManager) StartNewTask() {
	if currentTask := tm.currentTask.Load(); currentTask != nil {
		currentTask.Abort()
	}

	newTask := NewTranscribeTask()
	tm.currentTask.Store(newTask)

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
	result          atomic.Value // TranscriptionResult
}

func NewTranscribeTask() *TranscribeTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &TranscribeTask{
		ctx:    ctx,
		cancel: cancel,
		result: atomic.Value{},
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
	if result, ok := t.result.Load().(TranscriptionResult); ok {
		return result
	}
	return TranscriptionResult{}
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

				visibleText, err := nvimClient.GetVisibleText("{{CURSOR}}")
				if err != nil {
					log.Printf("Error getting visible text from nvim: %v", err)
					return
				}

				log.Printf("Inserting nvim context: %s", visibleText)

				description := fmt.Sprintf(
					"You are a voice to text typing assistant who is converting the audio to text to be inserted into a text editor with the following content. The cursor is located at {{CURSOR}}:\n%s",
					visibleText,
				)

				descriptionCh <- description
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

		t.result.Store(transcription)
		close(stateCh)
	}()

	return stateCh
}

func (tm *TaskManager) GetContext() string {
	if context, ok := tm.context.Load().(string); ok {
		return context
	}
	return ""
}

func (tm *TaskManager) SetContext(ctx string) {
	tm.context.Store(ctx)
}

func (tm *TaskManager) AppendToHistory(entry string) {
	tm.history = append(tm.history, entry)
}
