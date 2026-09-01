package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"flag"

	"github.com/getlantern/systray"
	"github.com/go-vgo/robotgo"
	"golang.design/x/hotkey"
)

var DEFAULT_TITLE = "TalkXTyper"

func main() {
	help := flag.Bool("help", false, "Show this help message")
	nvimTest := flag.String("nvim-test", "", "Test nvim integration (possible values: insertion, visible, mode, title)")
	oneShot := flag.Bool("one-shot", false, "Run the record task blocking in console, don't start any background systems")
	reportScreen := flag.Bool("report-screen", false, "Test screen description system, and exit")
	audioDevices := flag.Bool("audio-devices", false, "Print out all audio devices and exit")
	transcribeFname := flag.String("transcribe", "", "Transcribe audio from the specified file")

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if *audioDevices {
		log.Println("Available pipewire devices:")
		debugAudioDevices()
		return
	}

	if *nvimTest != "" {
		client := NewNvimClient()
		err := client.FindFirstNvim()

		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to find remote socket: %v\n", err)
			os.Exit(1)
		}

		var result string

		switch *nvimTest {
		case "insertion":
			result, err = client.GetInsertionText("<<CURSOR>>")
		case "visible":
			result, err = client.GetVisibleText()
		case "title":
			result, err = client.GetCurrentTitle()
		case "mode":
			var mode NvimMode
			mode, err = client.GetCurrentMode()
			result = string(mode)
		default:
			fmt.Fprintf(os.Stderr, "Invalid nvim-test value: %s\n", *nvimTest)
			os.Exit(1)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting visible text from nvim: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("nvim test result:", result)
		return
	}

	if *oneShot {
		oneShotMode()
		return
	}

	readConfig()

	if *transcribeFname != "" {
		// Read an audio file and send it to the transcription API.
		transcription, err := transcribeAudio(context.Background(), config.TranscriptionProvider, *transcribeFname, NewTranscriptionContext(""))
		if err != nil {
			log.Fatalf("Error transcribing file: %v", err)
		}

		fmt.Println("Transcription result:", transcription.String())

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
	log.SetOutput(os.Stderr)
	readConfig()

	// force disable any transcription fixing
	config.IncludeScreen = false
	config.IncludeNvim = false

	// live mode types as it transcribes, which would type into the console;
	// one-shot's contract is printing the transcript to stdout
	config.TranscriptionMode = TranscriptionModeBuffered

	log.Println("Now recording... (Press Ctrl+C or ESC to stop)")

	stopHotkey := hotkey.New(nil, hotkey.KeyEscape)

	systray.Run(func() {
		systray.SetIcon(icon_blue)
		systray.SetTitle(DEFAULT_TITLE)
		systray.SetTooltip("Ready")

		mAbort := systray.AddMenuItem("Abort", "Cancel operation and don't return anything")

		stopHotkey.Register()

		go func() {
			for {
				select {
				case state := <-taskManager.stateCh:
					switch state {
					case TaskStateRecording:
						systray.SetIcon(icon_red)
						systray.SetTitle("Recording")
						systray.SetTooltip("Recording audio...")
					case TaskStateTranscribing:
						systray.SetTooltip("Transcribing audio...")
						systray.SetIcon(icon_green)
					case TaskStateFinalizing:
						systray.SetTooltip("Finalizing live transcription...")
						systray.SetIcon(icon_green)
					default:
						systray.SetTooltip("Ready")
						systray.SetIcon(icon_blue)
					}
				case <-mAbort.ClickedCh:
					systray.Quit()
				}
			}
		}()

		taskManager.StartNewTask()

		// Listen for CTRL-C to stop the task
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)

		select {
		case <-c:
			break
		case <-stopHotkey.Keydown():
			stopHotkey.Unregister()
			break
		}

		log.Println("Stopping recording...")
		taskManager.StopRecording()

		log.Println("Waiting for transcription...")
		select {
		case transcription := <-taskManager.transcriptionRes:
			fmt.Println(transcription)
		case <-c:
			log.Println("CTRL-C received")
		}

		systray.Quit()
	}, func() {
		log.Println("Exiting...")
	})
}

func onReady() {
	systray.SetIcon(icon_blue)
	systray.SetTitle(DEFAULT_TITLE)
	systray.SetTooltip("Ready")

	// Register hotkeys first so menu titles can reflect them.
	toggleHotkey := hotkey.New([]hotkey.Modifier{hotkey.Mod1}, hotkey.KeyB)
	recordHotkeyLabel := ""
	if err := toggleHotkey.Register(); err != nil {
		notifyError("Could not register the record hotkey (Alt+B)", err)
	} else {
		log.Println("Toggle recording: Alt+B")
		recordHotkeyLabel = " (Alt+B)"
	}

	abortHotkey := hotkey.New([]hotkey.Modifier{hotkey.Mod1}, hotkey.KeyC)
	if err := abortHotkey.Register(); err != nil {
		notifyError("Could not register the abort hotkey (Alt+C)", err)
	} else {
		log.Println("Abort recording: Alt+C")
	}

	mRecord := systray.AddMenuItem("Record and Transcribe"+recordHotkeyLabel, "Start recording and transcribing")
	mAbort := systray.AddMenuItem("Abort Recording", "Abort the current recording")
	mAbort.Hide()
	mTranscriptionMode := systray.AddMenuItem("Transcription: "+transcriptionProfileLabel(config.TranscriptionProvider, config.TranscriptionMode), "Choose a transcription provider and delivery mode")
	mOpenAIBuffered := mTranscriptionMode.AddSubMenuItemCheckbox("Buffered — OpenAI gpt-transcribe", "Record first, then upload the completed audio file", transcriptionProfileSelected(TranscriptionProviderOpenAI, TranscriptionModeBuffered))
	mOpenAILive := mTranscriptionMode.AddSubMenuItemCheckbox("Live — OpenAI gpt-live-transcribe", "Type the transcript as it is finalized; no repair pass", transcriptionProfileSelected(TranscriptionProviderOpenAI, TranscriptionModeLive))
	mGeminiBuffered := mTranscriptionMode.AddSubMenuItemCheckbox("Buffered — Gemini 3.5 Transcribe", "Record first, then upload the completed audio file", transcriptionProfileSelected(TranscriptionProviderGemini, TranscriptionModeBuffered))
	mGeminiLive := mTranscriptionMode.AddSubMenuItemCheckbox("Live — Gemini 3.5 Transcribe", "Type text as you speak, correcting it as Gemini finalizes; no repair pass", transcriptionProfileSelected(TranscriptionProviderGemini, TranscriptionModeLive))
	transcriptionItems := []*systray.MenuItem{mOpenAIBuffered, mOpenAILive, mGeminiBuffered, mGeminiLive}
	setTranscriptionProfile := func(provider TranscriptionProvider, mode TranscriptionMode) {
		config.TranscriptionProvider = normalizeTranscriptionProvider(provider)
		config.TranscriptionMode = normalizeTranscriptionMode(mode)
		for _, item := range transcriptionItems {
			item.Uncheck()
		}
		switch {
		case provider == TranscriptionProviderGemini && mode == TranscriptionModeLive:
			mGeminiLive.Check()
		case provider == TranscriptionProviderGemini:
			mGeminiBuffered.Check()
		case mode == TranscriptionModeLive:
			mOpenAILive.Check()
		default:
			mOpenAIBuffered.Check()
		}
		mTranscriptionMode.SetTitle("Transcription: " + transcriptionProfileLabel(provider, mode))
		if err := writeConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		}
	}

	mGeminiSmartMode := systray.AddMenuItemCheckbox("Gemini smart mode", "Gemini removes filler words, false starts and repetitions and lightly formats the text. Verbatim when off.", config.GeminiSmartMode)

	mIncludeScreen := systray.AddMenuItemCheckbox("1. Include screen", "Highest priority. Sends a screenshot description; skips nvim/tmux when enabled.", config.IncludeScreen)
	mIncludeNvim := systray.AddMenuItemCheckbox("2. Include nvim", "Tried before tmux. Falls through to tmux if no active nvim is found.", config.IncludeNvim)
	mIncludeTmux := systray.AddMenuItemCheckbox("3. Include tmux", "Lowest priority. Used only if screen is off and nvim is off or unavailable.", config.IncludeTmux)

	var mOpenHTTP *systray.MenuItem
	if config.ListenAddress != "" {
		mOpenHTTP = systray.AddMenuItem("Open HTTP interface", "Open the HTTP interface in a browser")
	}

	mExit := systray.AddMenuItem("Exit", "Exit the application")

	var openHTTPCh chan struct{}
	if mOpenHTTP != nil {
		openHTTPCh = mOpenHTTP.ClickedCh
	}

	go func() {
		for {
			select {
			case state := <-taskManager.stateCh:
				switch state {
				case TaskStateRecording:
					systray.SetIcon(icon_red)
					if config.TranscriptionMode == TranscriptionModeLive {
						systray.SetTooltip("Recording and transcribing live...")
					} else {
						systray.SetTooltip("Recording audio (Buffered)...")
					}
					mRecord.SetTitle("Stop recording" + recordHotkeyLabel)
					mAbort.Show()
					for _, item := range transcriptionItems {
						item.Disable()
					}
				case TaskStateTranscribing:
					systray.SetTooltip("Transcribing audio...")
					systray.SetIcon(icon_green)
				case TaskStateFinalizing:
					systray.SetTooltip("Finalizing live transcription...")
					systray.SetIcon(icon_green)
				default:
					systray.SetTooltip("Ready")
					systray.SetIcon(icon_blue)
					mRecord.SetTitle("Record and Transcribe" + recordHotkeyLabel)
					mAbort.Hide()
					for _, item := range transcriptionItems {
						item.Enable()
					}
				}

			case transcription := <-taskManager.transcriptionRes:
				// Live mode already typed the transcript as it streamed; the
				// result here is only for history.
				if transcription.TranscriptionMode != TranscriptionModeLive {
					typeString(transcription.String())
				}

			case <-toggleHotkey.Keydown():
				taskManager.StartOrStopTask()

			case <-abortHotkey.Keydown():
				taskManager.Abort()

			case <-mRecord.ClickedCh:
				taskManager.StartOrStopTask()
			case <-mAbort.ClickedCh:
				taskManager.Abort()

			case <-mOpenAIBuffered.ClickedCh:
				setTranscriptionProfile(TranscriptionProviderOpenAI, TranscriptionModeBuffered)
			case <-mOpenAILive.ClickedCh:
				setTranscriptionProfile(TranscriptionProviderOpenAI, TranscriptionModeLive)
			case <-mGeminiBuffered.ClickedCh:
				setTranscriptionProfile(TranscriptionProviderGemini, TranscriptionModeBuffered)
			case <-mGeminiLive.ClickedCh:
				setTranscriptionProfile(TranscriptionProviderGemini, TranscriptionModeLive)

			case <-mGeminiSmartMode.ClickedCh:
				if mGeminiSmartMode.Checked() {
					mGeminiSmartMode.Uncheck()
				} else {
					mGeminiSmartMode.Check()
				}

				config.GeminiSmartMode = mGeminiSmartMode.Checked()

				if err := writeConfig(); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				}

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

			case <-mIncludeNvim.ClickedCh:
				if mIncludeNvim.Checked() {
					mIncludeNvim.Uncheck()
				} else {
					mIncludeNvim.Check()
				}

				config.IncludeNvim = mIncludeNvim.Checked()

				if err := writeConfig(); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				}

			case <-mIncludeTmux.ClickedCh:
				if mIncludeTmux.Checked() {
					mIncludeTmux.Uncheck()
				} else {
					mIncludeTmux.Check()
				}

				config.IncludeTmux = mIncludeTmux.Checked()

				if err := writeConfig(); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				}

			case <-openHTTPCh:
				if err := openHTTPInterface(config.ListenAddress); err != nil {
					notifyError("Could not open the HTTP interface", err)
				}

			case <-mExit.ClickedCh:
				systray.Quit()

			}
		}
	}()
}

func transcriptionModeLabel(mode TranscriptionMode) string {
	if normalizeTranscriptionMode(mode) == TranscriptionModeLive {
		return "Live"
	}
	return "Buffered"
}

func transcriptionProfileSelected(provider TranscriptionProvider, mode TranscriptionMode) bool {
	return normalizeTranscriptionProvider(config.TranscriptionProvider) == provider &&
		normalizeTranscriptionMode(config.TranscriptionMode) == mode
}

func transcriptionProfileLabel(provider TranscriptionProvider, mode TranscriptionMode) string {
	providerLabel := "OpenAI"
	if normalizeTranscriptionProvider(provider) == TranscriptionProviderGemini {
		providerLabel = "Gemini"
	}
	return providerLabel + " / " + transcriptionModeLabel(mode)
}

// typeCharDelayMillis is the extra pause robotgo adds after each typed
// character. robotgo's X11 backend already holds every key down for 5 ms, so
// this is set to zero for the fastest typing that still keeps events in order.
const typeCharDelayMillis = 0

func typeString(input string) error {
	robotgo.TypeStr(input, 0, typeCharDelayMillis)
	return nil
}

// typeBackspaces erases count characters. robotgo pauses KeySleep (10 ms by
// default) after every tap, which makes live corrections visibly lag, so the
// pause is shortened for the duration of the run.
func typeBackspaces(count int) {
	previousKeySleep := robotgo.KeySleep
	robotgo.KeySleep = 1
	defer func() { robotgo.KeySleep = previousKeySleep }()
	for i := 0; i < count; i++ {
		_ = robotgo.KeyTap("backspace")
	}
}

func openHTTPInterface(addr string) error {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	url := "http://" + host
	return exec.Command("xdg-open", url).Start()
}
