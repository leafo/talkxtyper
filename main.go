package main

import (
	"context"
	"fmt"
	"os"

	"flag"

	"github.com/getlantern/systray"
	"github.com/go-vgo/robotgo"
	"golang.design/x/hotkey"
)

var DEFAULT_TITLE = "TalkXTyper"

func main() {
	help := flag.Bool("help", false, "Show this help message")
	nvimTest := flag.Bool("nvim-test", false, "Test nvim integration")
	flag.Parse()

	if *help {
		fmt.Println("Usage: talkxtyper [options]")
		fmt.Printf("Starts the %s application in the system tray.\n", DEFAULT_TITLE)
		fmt.Println("Options:")
		fmt.Println("  --help        Show this help message")
		fmt.Println("  --nvim-test   Test nvim integration")
		return
	}

	if *nvimTest {
		client := NewNvimClient()
		err := client.FindFirstNvim()

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to find remote socket: %v\n", err)
			os.Exit(1)
		}
		result, err := client.GetVisibleText()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting visible text from nvim: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("nvim test result:", result)
		return
	}

	readConfig()

	if config.ListenAddress != "" {
		go startServer()
	}

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
				description, err := describeScreen(context.Background())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error describing screen: %v\n", err)
					continue
				}
				fmt.Printf("Description: %s\n", description)

			case <-mExit.ClickedCh:
				systray.Quit()

			}
		}
	}()
}

func typeString(input string) error {
	robotgo.TypeStr(input, 0, 2)
	return nil
}
