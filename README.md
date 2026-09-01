# TalkXTyper

TalkXTyper is a desktop application that will, on command, record your voice,
transcribe it using an OpenAI or Gemini transcription API, and "type" it to your
computer. It is activated with a global hotkey so that you do not lose focus on
the area you're typing into.

## Rationale

There are a few transcription tools out there, but I wanted to create my own so
I could explore different ideas based around my own workflow.

Although modern transcription models are very good, they do not automatically
know what is on the screen. For example, if you are coding and want to reference
a variable on the screen named `my_variable`, saying "my variable" may produce
"My variable" instead of the symbol on the screen.

### Attempts

1. **Send a desktop screenshot to the context model**
   - [x] Idea: take and send screenshot of the desktop while audio is being recorded,
   send image to gpt-4o to ask it to extract relevant textual features from the
   image. Combine the extracted information with the transcription output to attempt
   to fix the transcription to match text on the screen.
     - Result: vision processing can make the typing experience too slow
   - [ ] Use Claude Sonnet 3.5, it appears to be much faster with image processing

2. **Provide context to the transcription model**
   The `gpt-transcribe` API accepts a free-form `prompt` plus keyword and
   language hints. TalkXTyper passes available screen, Neovim, or tmux context
   into the transcription request, derives literal technical keyword hints,
   and retains a second repair pass for context-specific corrections. Submitted
   keywords are visible in the transcription history page.

3. **Extract text from running app**
   Idea: Query what the currently focused app is, then have custom code to
   extract the text from the screen.
   - [x] Implement text extraction from nvim using the `nvim` remote API
   - [ ] Explore extracting text from browser. (Consider a browser extension)

## Hotkeys

TalkXTyper registers the following global hotkeys:

- `Alt+B`: Toggle recording. Press once to start recording, press again to stop
  recording and begin transcription.
- `Alt+C`: Abort the current task. This cancels the operation regardless of
  state, so it works both while recording and while a transcription is already
  in flight. Aborting discards the result and types nothing.

These actions are also available from the systray menu ("Record and Transcribe"
and "Abort Recording").

The systray's **Transcription** submenu switches between four profiles:

- **Buffered — OpenAI gpt-transcribe** and **Buffered — Gemini 3.5
  Transcribe** record the complete utterance and upload it after you stop.
- **Live — OpenAI gpt-live-transcribe** streams 24 kHz PCM and types
  incremental transcript deltas.
- **Live — Gemini 3.5 Transcribe** streams 16 kHz PCM and types Gemini's
  interim hypothesis as you speak. When Gemini revises or finalizes a phrase,
  the changed tail is backspaced and retyped, so the text on screen can
  briefly change while you talk.

The selected provider and mode are saved in the configuration file. Keyword and
language hints and MP3 history are used with every profile. OpenAI also receives
the free-form context prompt; Gemini receives the extracted terms as custom
vocabulary. The optional repair pass only runs in buffered mode: live mode types
text as you speak and only corrects it with the provider's own final transcript.

## Configuration

The configuration for TalkXTyper is stored in a JSON file located in your user
configuration directory. The file is named `talkxtyper-config.json`.

### Configuration Options

- `OpenAIKey`: Your API key for the OpenAI API.
- `GeminiKey`: Your Gemini API key. `GEMINI_API_KEY` and `GOOGLE_API_KEY`
  environment variables take precedence over this value.
- `TranscriptionProvider`: `"openai"` (the default) or `"gemini"`.
- `TranscriptionMode`: `"buffered"` (the default) or `"live"`.
- `IncludeScreen`: A boolean value indicating whether to analyze the screen to augment the transcription. The config file will be updated automatically if you change this value in the program.
- `IncludeNvim`: A boolean value indicating whether to analyze the screen to augment the transcription.
- `IncludeTmux`: A boolean value indicating whether to collect context from the active tmux pane.
- `GeminiSmartMode`: When `true`, Gemini transcription removes filler words,
  false starts and repetitions and applies light formatting instead of
  transcribing verbatim. Applies to both Gemini profiles. Toggle it from the
  tray menu.
- `Keywords`: A list of terms to always send as transcription hints, such as
  names, identifiers, and jargon you say often. They are sent ahead of any
  keywords extracted from the collected context. The web interface has a
  `/keywords` page for editing this list.

Screen description and the buffered context-repair pass still use OpenAI, even
when Gemini is selected for transcription. An OpenAI key is therefore also
required when those context features are enabled.

## Web interface

`ListenAddress` can be specified in the config file to enable the web
interface. The web interface includes some experimental functionality. The web
interface is not enabled by default.

Eg. Setting `ListenAddress` to `"localhost:9898"` will make the web interface
accessible at `http://localhost:9898`.

SECURITY NOTE: The web interface adds a HTTP API for controlling recording and
transcribing, in addition to taking screenshots of the desktop. Don't leave it
running if you don't need it.

The web interface exposes a way to review transcription history via `/history`
and listen to the audio files that were recorded. You can use this to debug if
recording is working as expected.

## Installation

To install TalkXTyper, you will need to have Go installed. Run the following command:

    go install github.com/leafo/talkxtyper@latest

This project has only been tested on Linux, but it uses cross-platform libraries, so it should work on other platforms.

## License

This project is licensed under the MIT License. See the LICENSE file for details.
