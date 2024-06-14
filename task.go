package main

import (
	"context"
	"fmt"
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
	currentTask      atomic.Pointer[Task]
	transcriptionRes chan string
	stateCh          chan TaskState
}

// task managers ensures only only one task is running at a time and cancels
// the current task if a new one is started
var taskManager = TaskManager{
	currentTask:      atomic.Pointer[Task]{}, // Initialize as nil
	transcriptionRes: make(chan string),
	stateCh:          make(chan TaskState),
}

func (tm *TaskManager) StartNewTask() {
	if currentTask := tm.currentTask.Load(); currentTask != nil {
		currentTask.Abort()
	}

	newTask := NewTask()
	tm.currentTask.Store(newTask)

	stateCh := newTask.Start()

	go func() {
		for state := range stateCh {
			tm.stateCh <- state
		}

		tm.stateCh <- TaskStateIdle
		tm.currentTask.CompareAndSwap(newTask, nil)
		if result, ok := newTask.result.Load().(string); ok {
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

type Task struct {
	stopRecordingCh chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	result          atomic.Value // string
}

func NewTask() *Task {
	ctx, cancel := context.WithCancel(context.Background())
	return &Task{
		ctx:    ctx,
		cancel: cancel,
		result: atomic.Value{},
	}
}

// stop the recording so that transcription can be started
func (t *Task) StopRecording() {
	if t.stopRecordingCh != nil {
		close(t.stopRecordingCh)
		t.stopRecordingCh = nil
	}
}

// cancel the task, regardless of state
func (t *Task) Abort() {
	t.cancel()
}

func (t *Task) Start() chan TaskState {
	t.stopRecordingCh = make(chan struct{})
	stateCh := make(chan TaskState)

	go func() {
		stateCh <- TaskStateRecording
		recordingBuffer, err := recordAudio(t.ctx, t.stopRecordingCh)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			close(stateCh)
			return
		}

		mp3FileName, err := writeRecordingToMP3(recordingBuffer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing MP3 file: %v\n", err)
			close(stateCh)
			return
		}
		defer os.Remove(mp3FileName)

		stateCh <- TaskStateTranscribing
		transcription, err := transcribeAudio(t.ctx, mp3FileName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error transcribing audio: %v\n", err)
			close(stateCh)
			return
		}
		fmt.Printf("Transcription: %s\n", transcription)

		t.result.Store(transcription)
		close(stateCh)
	}()

	return stateCh
}
