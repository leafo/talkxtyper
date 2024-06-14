package main

import (
	"fmt"
	"os"
	"time"

	"github.com/getlantern/systray"
	"github.com/go-vgo/robotgo"
	"golang.design/x/hotkey"

	"io/ioutil"
)

var DEFAULT_TITLE = "TalkXTyper"

func main() {
	readConfig()
	onExit := func() {
		fmt.Println("Exiting...")
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(icon_blue)
	systray.SetTitle(DEFAULT_TITLE)
	systray.SetTooltip("Ready")

	mRecord := systray.AddMenuItem("Record and Transcribe", "Start recording and transcribing")
	mAbort := systray.AddMenuItem("Abort Recording", "Abort the current recording")
	mAbort.Hide()

	mIncludeScreen := systray.AddMenuItemCheckbox("Include screen", "Analyze the screen to augment the transcription", config.IncludeScreen)

	mReportScreen := systray.AddMenuItem("Report screen", "Snapshot the current screen")
	mExit := systray.AddMenuItem("Exit", "Exit the application")

	// setup hotkeys
	hk := hotkey.New([]hotkey.Modifier{hotkey.Mod1}, hotkey.KeyB)
	hk.Register()

	go func() {
		for {
			select {
			case state := <-taskManager.stateCh:
				switch state {
				case TaskStateRecording:
					systray.SetIcon(icon_red)
					systray.SetTooltip("Recording audio...")
					mRecord.SetTitle("Stop recording")
					mAbort.Show()
				case TaskStateTranscribing:
					systray.SetTooltip("Transcribing audio...")
					systray.SetIcon(icon_green)
				default:
					systray.SetTooltip("Ready")
					systray.SetIcon(icon_blue)
					mRecord.SetTitle("Record and Transcribe")
					mAbort.Hide()
				}

			case transcription := <-taskManager.transcriptionRes:
				typeString(transcription)

			case <-hk.Keydown():
				taskManager.StartOrStopTask()
			case <-mRecord.ClickedCh:
				taskManager.StartOrStopTask()
			case <-mAbort.ClickedCh:
				taskManager.Abort()

			case <-mIncludeScreen.ClickedCh:
				if mIncludeScreen.Checked() {
					mIncludeScreen.Uncheck()
				} else {
					mIncludeScreen.Check()
				}

				config.IncludeScreen = mIncludeScreen.Checked()

				if err := writeConfig(); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				}

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
