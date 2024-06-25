package main

import (
	"context"
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
	transcriptionRes chan string
	stateCh          chan TaskState
	context          atomic.Value // string
	history          []string     // history of transcriptions
}

// task managers ensures only only one task is running at a time and cancels
// the current task if a new one is started
var taskManager = TaskManager{
	currentTask:      atomic.Pointer[TranscribeTask]{}, // Initialize as nil
	transcriptionRes: make(chan string),
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

		if result := newTask.GetResult(); result != "" {
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
	result          atomic.Value // string
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

func (t *TranscribeTask) GetResult() string {
	if result, ok := t.result.Load().(string); ok {
		return result
	}
	return ""
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
		log.Printf("Transcription: %s\n", transcription)

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
