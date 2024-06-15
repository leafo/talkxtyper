package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	portaudio "github.com/gordonklaus/portaudio"
	"github.com/viert/go-lame"
)

const sampleRate = 44100
const bufferSize = 256
const maxRecordSeconds = 30
const debug = false

func recordAudio(ctx context.Context, stopCh <-chan struct{}) ([]int16, error) {
	ctx, cancel := context.WithTimeout(ctx, maxRecordSeconds*time.Second)
	defer cancel()

	err := portaudio.Initialize()
	if err != nil {
		return nil, fmt.Errorf("Error initializing PortAudio: %v", err)
	}
	defer portaudio.Terminate()

	var recordingBuffer []int16
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, bufferSize, func(in []int16) {
		if debug {
			fmt.Printf("Chunk length: %d\n", len(in))
			fmt.Printf("Input chunk: %+v\n", in)
		}

		recordingBuffer = append(recordingBuffer, in...)
	})

	if err != nil {
		return nil, fmt.Errorf("Error opening default stream: %v", err)
	}
	defer stream.Close()

	if err := stream.Start(); err != nil {
		return nil, fmt.Errorf("Error starting stream: %v", err)
	}
	defer stream.Stop()

	fmt.Println("Recording, waiting for stop signal...")
	select {
	case <-stopCh:
		stream.Stop()
		fmt.Println("Recording finished.")
	case <-ctx.Done():
		stream.Stop()
		return nil, fmt.Errorf("Recording cancelled")
	}

	return recordingBuffer, nil
}

// encode and write an audio recording to a MP3 file to a temporary file path
// and return the path
func writeRecordingToMP3(recordingBuffer []int16) (string, error) {
	// Convert int16 buffer to byte buffer
	byteBuffer := new(bytes.Buffer)
	for _, sample := range recordingBuffer {
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
	encoder.SetInSamplerate(sampleRate)
	defer encoder.Close()

	// Encode to MP3
	if _, err := io.Copy(encoder, byteBuffer); err != nil {
		return "", fmt.Errorf("Error encoding MP3: %v", err)
	}

	return tempFile.Name(), nil
}

func playRecording(recordingBuffer []int16) error {
	outputDevice, err := findOutputDeviceByName("pipewire")
	if err != nil {
		return fmt.Errorf("Error finding output device: %v", err)
	}

	if err := playRecordingToDevice(recordingBuffer, outputDevice); err != nil {
		return fmt.Errorf("Error during playback: %v", err)
	}

	return nil
}

func findOutputDeviceByName(name string) (*portaudio.DeviceInfo, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("Error listing devices: %v", err)
	}

	for _, device := range devices {
		if device.Name == name && device.MaxOutputChannels > 0 {
			return device, nil
		}
	}
	return nil, fmt.Errorf("Output device '%s' not found", name)
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
		fmt.Printf("Playback stream: %+v\n", playbackStream)
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
