package main

import "sync/atomic"

type TaskState int

const (
	TaskStateIdle TaskState = iota
	TaskStateRecording
	TaskStateTranscribing
)

const maxHistoryLength = 100

// TaskManager is a thread safe manager for global task state
type TaskManager struct {
	currentTask      atomic.Pointer[TranscribeTask]
	transcriptionRes chan *TranscriptionResult
	stateCh          chan TaskState
	context          atomic.Pointer[string]
	history          atomic.Pointer[[]*TranscriptionResult]
}

// task managers ensures only only one task is running at a time and cancels
// the current task if a new one is started
var taskManager = TaskManager{
	currentTask:      atomic.Pointer[TranscribeTask]{}, // Initialize as nil
	transcriptionRes: make(chan *TranscriptionResult),
	stateCh:          make(chan TaskState, 10),
	context:          atomic.Pointer[string]{},
	history:          atomic.Pointer[[]*TranscriptionResult]{},
}

func (tm *TaskManager) StartNewTask() *TranscribeTask {
	newTask := NewTranscribeTask()

	oldTask := tm.currentTask.Swap(newTask)
	if oldTask != nil {
		oldTask.Abort()
	}

	stateCh := newTask.Start()

	go func() {
		// this waits for task to fish, state is closed when task is done
		for state := range stateCh {
			tm.stateCh <- state
		}

		tm.stateCh <- TaskStateIdle
		// either publish the result to the task listener or send it to task manager

		if tm.currentTask.CompareAndSwap(newTask, nil) {
			if result := newTask.GetResult(); !result.IsEmpty() {
				tm.transcriptionRes <- result
			}
		}
	}()

	return newTask
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

func (tm *TaskManager) GetContext() string {
	return *tm.context.Load()
}

func (tm *TaskManager) SetContext(ctx string) {
	tm.context.Store(&ctx)
}

func (tm *TaskManager) AppendToHistory(entry *TranscriptionResult) {
	for {
		oldHistory := tm.history.Load()
		if oldHistory == nil {
			newHistory := []*TranscriptionResult{entry}
			if tm.history.CompareAndSwap(nil, &newHistory) {
				break
			}
		} else {
			newHistory := append(*oldHistory, entry)
			if len(newHistory) > maxHistoryLength {
				newHistory = newHistory[len(newHistory)-maxHistoryLength:]
			}
			if tm.history.CompareAndSwap(oldHistory, &newHistory) {
				break
			}
		}
	}
}

// get a copy of the current history
func (tm *TaskManager) GetHistory() []*TranscriptionResult {
	history := tm.history.Load()
	if history == nil {
		return []*TranscriptionResult{}
	}
	return append([]*TranscriptionResult(nil), *history...)
}
