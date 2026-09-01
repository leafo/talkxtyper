package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"sync"
	"time"

	portaudio "github.com/gordonklaus/portaudio"
	"github.com/viert/go-lame"
)

const sampleRate = 44100
const openAILiveSampleRate = 24000
const bufferSize = 256
// Safety net for a forgotten hot mic. Hitting it acts like pressing stop:
// the recording is kept and transcribed, not discarded.
const maxRecordSeconds = 300
const minRecordSeconds = 1
const debug = false

type AudioRecording struct {
	Samples    []int16
	SampleRate int
}

// recordAudioStream captures mono PCM16 audio. When onChunk is non-nil, copied
// chunks are delivered from a worker goroutine so PortAudio's callback never
// blocks on network I/O.
func recordAudioStream(parentCtx context.Context, stopCh <-chan struct{}, recordingSampleRate int, onChunk func([]int16) error) (AudioRecording, error) {
	ctx, cancel := context.WithTimeout(parentCtx, maxRecordSeconds*time.Second)
	defer cancel()

	err := portaudio.Initialize()
	if err != nil {
		return AudioRecording{}, fmt.Errorf("Error initializing PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	// TODO: allow user to specify input device, the default is failing on my computer
	inputDevice, err := findDeviceByName("pipewire")
	if err != nil {
		return AudioRecording{}, fmt.Errorf("Error finding input device: %v", err)
	}

	var recordingBuffer []int16
	var chunkCh chan []int16
	var senderDone chan error
	if onChunk != nil {
		// Enough capacity for the full maximum recording prevents a slow socket
		// from blocking or dropping audio in the realtime callback.
		chunkCapacity := maxRecordSeconds*recordingSampleRate/bufferSize + 128
		chunkCh = make(chan []int16, chunkCapacity)
		senderDone = make(chan error, 1)
		go func() {
			for chunk := range chunkCh {
				if err := onChunk(chunk); err != nil {
					senderDone <- err
					cancel()
					return
				}
			}
			senderDone <- nil
		}()
	}

	var queueErr error
	var queueErrMu sync.Mutex
	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDevice,
			Channels: 1,
			Latency:  inputDevice.DefaultLowInputLatency,
		},
		SampleRate:      float64(recordingSampleRate),
		FramesPerBuffer: bufferSize,
	}, func(in []int16) {
		if debug {
			log.Printf("Chunk length: %d\n", len(in))
			log.Printf("Input chunk: %+v\n", in)
		}

		recordingBuffer = append(recordingBuffer, in...)
		if chunkCh != nil {
			chunk := append([]int16(nil), in...)
			select {
			case chunkCh <- chunk:
			default:
				queueErrMu.Lock()
				if queueErr == nil {
					queueErr = fmt.Errorf("live audio queue is full")
					cancel()
				}
				queueErrMu.Unlock()
			}
		}
	})

	if err != nil {
		if chunkCh != nil {
			close(chunkCh)
			<-senderDone
		}
		return AudioRecording{}, fmt.Errorf("Error opening default stream: %v", err)
	}
	defer stream.Close()

	startTime := time.Now()

	if err := stream.Start(); err != nil {
		if chunkCh != nil {
			close(chunkCh)
			<-senderDone
		}
		return AudioRecording{}, fmt.Errorf("Error starting stream: %v", err)
	}
	defer stream.Stop()

	if err := playReadyBeep(); err != nil {
		log.Printf("Failed to play ready beep: %v", err)
	}

	log.Println("Recording, waiting for stop signal...")
	select {
	case <-stopCh:
		stream.Stop()
		log.Println("Recording finished.")
		// The input stream is already stopped, so the microphone cannot pick
		// the stop beep up.
		playStopBeepAsync()
	case <-ctx.Done():
		stream.Stop()
	}

	if chunkCh != nil {
		close(chunkCh)
		if err := <-senderDone; err != nil {
			return AudioRecording{}, fmt.Errorf("Error streaming audio: %v", err)
		}
	}

	queueErrMu.Lock()
	err = queueErr
	queueErrMu.Unlock()
	if err != nil {
		return AudioRecording{}, err
	}
	if ctx.Err() != nil {
		if parentCtx.Err() != nil {
			return AudioRecording{}, fmt.Errorf("Recording cancelled")
		}
		log.Printf("Max recording length reached (%ds), stopping", maxRecordSeconds)
	}

	if time.Since(startTime) < minRecordSeconds*time.Second {
		return AudioRecording{}, fmt.Errorf("Aborting, recording too short: < %d seconds", minRecordSeconds)
	}

	return AudioRecording{Samples: recordingBuffer, SampleRate: recordingSampleRate}, nil
}

// encode and write an audio recording to a MP3 file to a temporary file path
// and return the path
func writeAudioRecordingToMP3(recording AudioRecording) (string, error) {
	// Convert int16 buffer to byte buffer
	byteBuffer := new(bytes.Buffer)
	for _, sample := range recording.Samples {
		byteBuffer.WriteByte(byte(sample & 0xff))
		byteBuffer.WriteByte(byte((sample >> 8) & 0xff))
	}

	// Write to temporary file
	tempFile, err := ioutil.TempFile("", fmt.Sprintf("talkxtyper-%d-*.mp3", time.Now().Unix()))
	if err != nil {
		return "", fmt.Errorf("Error creating temporary file: %v", err)
	}
	defer tempFile.Close()

	// Initialize LAME encoder with the output file handle
	encoder := lame.NewEncoder(tempFile)
	encoder.SetNumChannels(1)
	encoder.SetInSamplerate(recording.SampleRate)
	defer encoder.Close()

	// Encode to MP3
	if _, err := io.Copy(encoder, byteBuffer); err != nil {
		return "", fmt.Errorf("Error encoding MP3: %v", err)
	}

	return tempFile.Name(), nil
}

// playReadyBeep plays a short tone through the default output device to
// signal that the input stream is live and capture has begun. The microphone
// may briefly pick up the beep, but the user should only start speaking
// after hearing it, so their speech is captured intact.
func playReadyBeep() error {
	return playTones([]toneSegment{{freq: 880, durationMs: 120}})
}

// playStopBeep plays a short descending tone to confirm that recording has
// stopped and nothing more will be captured.
func playStopBeep() error {
	return playTones([]toneSegment{{freq: 660, durationMs: 80}, {freq: 440, durationMs: 100}})
}

// playStopBeepAsync plays the stop beep without blocking finalization. It
// holds its own PortAudio reference because the recording stream's
// initialization is released as soon as recordAudioStream returns.
func playStopBeepAsync() {
	go func() {
		if err := portaudio.Initialize(); err != nil {
			log.Printf("Failed to initialize PortAudio for stop beep: %v", err)
			return
		}
		defer portaudio.Terminate()
		if err := playStopBeep(); err != nil {
			log.Printf("Failed to play stop beep: %v", err)
		}
	}()
}

type toneSegment struct {
	freq       float64
	durationMs int
}

// renderTones synthesizes the segments back to back as 16-bit mono PCM at the
// recording sample rate, with a short fade at each segment edge to avoid
// clicks.
func renderTones(segments []toneSegment) []int16 {
	const amplitude = 0.25
	var samples []int16
	for _, segment := range segments {
		numSamples := sampleRate * segment.durationMs / 1000
		fadeLen := numSamples / 8
		for i := 0; i < numSamples; i++ {
			envelope := 1.0
			if i < fadeLen {
				envelope = float64(i) / float64(fadeLen)
			} else if i > numSamples-fadeLen {
				envelope = float64(numSamples-i) / float64(fadeLen)
			}
			samples = append(samples, int16(math.Sin(2*math.Pi*segment.freq*float64(i)/float64(sampleRate))*amplitude*envelope*32767))
		}
	}
	return samples
}

func playTones(segments []toneSegment) error {
	outputDevice, err := findDeviceByName("pipewire")
	if err != nil {
		return fmt.Errorf("Error finding output device for beep: %v", err)
	}

	samples := renderTones(segments)
	totalMs := 0
	for _, segment := range segments {
		totalMs += segment.durationMs
	}

	idx := 0
	done := make(chan struct{})
	var doneOnce sync.Once
	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDevice,
			Channels: 1,
			Latency:  outputDevice.DefaultLowOutputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: bufferSize,
	}, func(out []int16) {
		for i := range out {
			if idx < len(samples) {
				out[i] = samples[idx]
				idx++
			} else {
				out[i] = 0
			}
		}
		if idx >= len(samples) {
			doneOnce.Do(func() { close(done) })
		}
	})

	if err != nil {
		return fmt.Errorf("Error opening beep stream: %v", err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return fmt.Errorf("Error starting beep stream: %v", err)
	}
	defer stream.Stop()

	select {
	case <-done:
	case <-time.After(time.Duration(totalMs+200) * time.Millisecond):
	}
	// Let the output buffer drain so the tail of the tone isn't clipped.
	time.Sleep(40 * time.Millisecond)

	return nil
}

func playRecording(recordingBuffer []int16) error {
	outputDevice, err := findDeviceByName("pipewire")
	if err != nil {
		return fmt.Errorf("Error finding output device: %v", err)
	}

	if err := playRecordingToDevice(recordingBuffer, outputDevice); err != nil {
		return fmt.Errorf("Error during playback: %v", err)
	}

	return nil
}

func findDeviceByName(name string) (*portaudio.DeviceInfo, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("Error listing devices: %v", err)
	}
	for _, device := range devices {
		if device.Name == name {
			return device, nil
		}
	}
	return nil, fmt.Errorf("Output device '%s' not found", name)
}

// init portaudio and print out all deivices with their names and number of inputs and outpuits
func debugAudioDevices() {
	err := portaudio.Initialize()
	if err != nil {
		log.Fatalf("Error initializing PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	devices, err := portaudio.Devices()
	if err != nil {
		log.Fatalf("Error listing devices: %v", err)
	}

	for _, device := range devices {
		fmt.Printf("Name: %s, MaxInputChannels: %d, MaxOutputChannels: %d\n", device.Name, device.MaxInputChannels, device.MaxOutputChannels)
	}
}

func playRecordingToDevice(recordingBuffer []int16, outputDevice *portaudio.DeviceInfo) error {
	// play back the recording using portaudio, 44100 Hz, 16 bit signed mono audio
	playbackStream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDevice,
			Channels: 1,
			Latency:  outputDevice.DefaultLowOutputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: bufferSize,
	}, func(out []int16) {
		for i := range out {
			if len(recordingBuffer) > 0 {
				out[i] = recordingBuffer[0]
				recordingBuffer = recordingBuffer[1:]
			} else {
				out[i] = 0
			}
		}
	})

	if err != nil {
		return fmt.Errorf("Error opening playback stream: %v", err)
	}
	defer playbackStream.Close()

	if debug {
		log.Printf("Playback stream: %+v\n", playbackStream)
	}

	if err := playbackStream.Start(); err != nil {
		return fmt.Errorf("Error starting playback stream: %v", err)
	}
	defer playbackStream.Stop()

	stop := make(chan struct{})
	go func() {
		fmt.Println("Playing (press 'Enter' to stop)...")
		fmt.Scanln()
		close(stop)
	}()

	for len(recordingBuffer) > 0 {
		select {
		case <-stop:
			return nil
		default:
			// continue playback
		}
	}

	return nil
}
