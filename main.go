package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-vgo/robotgo"
	portaudio "github.com/gordonklaus/portaudio"
	"github.com/sashabaranov/go-openai"

	"bytes"
	"io/ioutil"

	"github.com/viert/go-lame"
)

const sampleRate = 44100
const bufferSize = 256
const maxRecordSeconds = 10

const debug = false

func main() {
	err := portaudio.Initialize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing PortAudio: %v\n", err)
		os.Exit(1)
	}
	defer portaudio.Terminate()

	recordingBuffer, err := recordAudio()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	mp3FileName, err := writeRecordingToMP3(recordingBuffer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing MP3 file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("MP3 file saved as: %s\n", mp3FileName)

	defer os.Remove(mp3FileName)

	transcription, err := transcribeAudio(mp3FileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error transcribing audio: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Transcription: %s\n", transcription)

	if err := typeString(transcription); err != nil {
		fmt.Fprintf(os.Stderr, "Error typing transcription: %v\n", err)
		os.Exit(1)
	}

	// outputDevice, err := findOutputDeviceByName("pipewire")
	// if err != nil {
	// 	fmt.Fprintf(os.Stderr, "%v\n", err)
	// 	os.Exit(1)
	// }

	// if err := playbackBuffer(recordingBuffer, outputDevice); err != nil {
	// 	fmt.Fprintf(os.Stderr, "Error during playback: %v\n", err)
	// 	os.Exit(1)
	// }
}

func typeString(input string) error {
	robotgo.TypeStr(input, 0, 16)
	return nil
}

func transcribeAudio(mp3FilePath string) (string, error) {
	// Initialize OpenAI client
	apiKey := os.Getenv("OPENAI_API_KEY")
	client := openai.NewClient(apiKey)

	// Create a request for transcription
	req := openai.AudioRequest{
		FilePath:    mp3FilePath,
		Model:       "whisper-1",
		Language:    "en",
		Temperature: 0.5,
	}

	// Perform the transcription
	resp, err := client.CreateTranscription(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("Error sending transcription request: %v", err)
	}

	return resp.Text, nil
}

func recordAudio() ([]int16, error) {
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

	fmt.Println("Recording, press 'Enter' to stop...")
	fmt.Scanln()
	stream.Stop()
	fmt.Println("Stream stopped.")

	return recordingBuffer, nil
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

func writeRecordingToMP3(recordingBuffer []int16) (string, error) {
	// Convert int16 buffer to byte buffer
	byteBuffer := new(bytes.Buffer)
	for _, sample := range recordingBuffer {
		byteBuffer.WriteByte(byte(sample & 0xff))
		byteBuffer.WriteByte(byte((sample >> 8) & 0xff))
	}

	// Initialize LAME encoder
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

func playbackBuffer(recordingBuffer []int16, outputDevice *portaudio.DeviceInfo) error {
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
