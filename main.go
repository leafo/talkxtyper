package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"flag"

	"github.com/getlantern/systray"
	"github.com/go-vgo/robotgo"
	"golang.design/x/hotkey"
)

var DEFAULT_TITLE = "TalkXTyper"

func main() {
	help := flag.Bool("help", false, "Show this help message")
	nvimTest := flag.Bool("nvim-test", false, "Test nvim integration")
	oneShot := flag.Bool("one-shot", false, "Run the record task blocking in console, don't start any background systems")
	reportScreen := flag.Bool("report-screen", false, "Test screen description system, and exit")
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
		result, err := client.GetVisibleText("<<CURSOR>>")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting visible text from nvim: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("nvim test result:", result)
		return
	}

	readConfig()

	if *oneShot {
		oneShotMode()
		return
	}

	if *reportScreen {
		description, err := describeScreen(context.Background())
		if err != nil {
			log.Fatalf("Error describing screen: %v", err)
		}
		log.Printf("Description: %s", description)
		return
	}

	if config.ListenAddress != "" {
		go startServer()
	}

	onExit := func() {
		log.Println("Exiting...")
	}
	// note this takes over the main loop
	systray.Run(onReady, onExit)
}

func oneShotMode() {
	log.Println("Now recording... (Press Ctrl+C to stop)")
	taskManager.StartOrStopTask()

	// Listen for CTRL-C to stop the task
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c

	log.Println("Stopping recording...")
	taskManager.StopRecording()

	log.Println("Waiting for transcription...")
	select {
	case transcription := <-taskManager.transcriptionRes:
		fmt.Println(transcription)
	case <-c:
		log.Println("CTRL-C received")
	}
}

func onReady() {
	systray.SetIcon(icon_blue)
	systray.SetTitle(DEFAULT_TITLE)
	systray.SetTooltip("Ready")

	mRecord := systray.AddMenuItem("Record and Transcribe", "Start recording and transcribing")
	mAbort := systray.AddMenuItem("Abort Recording", "Abort the current recording")
	mAbort.Hide()

	mIncludeScreen := systray.AddMenuItemCheckbox("Include screen", "Analyze the screen to augment the transcription", config.IncludeScreen)

	mExit := systray.AddMenuItem("Exit", "Exit the application")

	// setup hotkeys
	listen1 := hotkey.New([]hotkey.Modifier{hotkey.Mod1}, hotkey.KeyB)
	listen1.Register()

	listen2 := hotkey.New([]hotkey.Modifier{hotkey.Mod1}, hotkey.KeyC)
	listen2.Register()

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

			case <-listen1.Keydown():
				taskManager.StartOrStopTask()

			case <-listen2.Keydown():
				taskManager.Abort()

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
