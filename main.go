package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/getlantern/systray"
	"github.com/go-vgo/robotgo"

	"io/ioutil"
)

func main() {
	readConfig()
	onExit := func() {
		fmt.Println("Exiting...")
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(icon_blue)
	systray.SetTitle("TalkXTyper")
	systray.SetTooltip("TalkXTyper")

	mRecord := systray.AddMenuItem("Record and Transcribe", "Start recording and transcribing")
	mAbort := systray.AddMenuItem("Abort Recording", "Abort the current recording")
	mAbort.Hide()

	mReportScreen := systray.AddMenuItem("Report screen", "Snapshot the current screen")
	mExit := systray.AddMenuItem("Exit", "Exit the application")

	var stopCh chan struct{}

	var isAborted atomic.Bool

	go func() {
		for {
			select {
			case <-mRecord.ClickedCh:
				systray.SetIcon(icon_red)
				if stopCh == nil {
					mRecord.SetTitle("Stop recording")
					mAbort.Show()

					stopCh = make(chan struct{})
					go func() {
						recordingBuffer, err := recordAudio(stopCh)
						if err != nil {
							fmt.Fprintf(os.Stderr, "%v\n", err)
							return
						}

						mp3FileName, err := writeRecordingToMP3(recordingBuffer)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error writing MP3 file: %v\n", err)
							return
						}
						defer os.Remove(mp3FileName)

						transcription, err := transcribeAudio(mp3FileName)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error transcribing audio: %v\n", err)
							return
						}
						fmt.Printf("Transcription: %s\n", transcription)

						if err := typeString(transcription); err != nil {
							fmt.Fprintf(os.Stderr, "Error typing transcription: %v\n", err)
						}
						stopCh = nil
					}()
				} else {
					close(stopCh)
					stopCh = nil
					systray.SetIcon(icon_blue)
					mRecord.SetTitle("Record and Transcribe")
					mAbort.Hide()
				}

			case <-mAbort.ClickedCh:
				isAborted.Store(true)
			case <-mReportScreen.ClickedCh:
				path, err := takeScreenshot()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error taking screenshot: %v\n", err)
					continue
				}
				fmt.Printf("Screenshot: %s\n", path)

				defer os.Remove(path)

				transcription, err := describeImage(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error describing image: %v\n", err)
					continue
				}
				fmt.Printf("Transcription: %s\n", transcription)

			case <-mExit.ClickedCh:
				systray.Quit()

			}
		}
	}()
}

func typeString(input string) error {
	robotgo.TypeStr(input, 0, 16)
	return nil
}

func takeScreenshot() (string, error) {
	// Capture the screen
	image := robotgo.CaptureImg()

	// Create a temporary file
	tempFile, err := ioutil.TempFile("", fmt.Sprintf("talkxtyper-%d-*.png", time.Now().Unix()))
	if err != nil {
		return "", fmt.Errorf("Error creating temporary file: %v", err)
	}
	defer tempFile.Close()

	// Save the screenshot to the temporary file
	err = robotgo.Save(image, tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("Error saving screenshot: %v", err)
	}

	return tempFile.Name(), nil
}
