# TalkXTyper

TalkXTyper is a desktop application that will, on command, record your voice,
transcribe it using the OpenAI Whisper API, and "type" it to your computer. It
is activated with a global hotkey so that you do not lose focus on the area
you're typing into.

## Rationale

There are a few transcription tools out there, but I wanted to create my own so
I could explore different ideas based around my own workflow.

Although Whisper is very good, it lacks context for what is going on on the
screen. For example, if you are coding and want to reference a variable on the
screen named `my_variable`, saying "my variable" will often produce "My
variable" instead of the symbol on the screen.

### Attempts

1. **Send screenshot of desktop to to gpt-4o**
   - [*] Idea: take and send screenshot of the desktop while audio is being recorded,
   send image to gpt-4o to ask it to extract relevant textual features from the
   image. Combine the extracted information with the whisper output to attempt
   to fix the transcription to match text on the screen.
   Resut: gpt-4o with vision is too slow, it makes the typing experience too slow
   - [ ] Use Claude Sonnet 3.5, it appears to be much faster with image processing

2. **Using the `prompt` parameter with Whisper API**
   The whisper API includes a `prompt` parameter that can be used for basic
   instruction during transcription. The results were poor and the max size is
   short. Haven't found a use for it

3. **Extract text from running app**
   Idea: Query what the currently focused app is, then have custom code to
   extract the text from the screen.
   - [*] Implement text extraction from nvim using the `nvim` remote API
   - [ ] Explore extracting text from browser. (Consider a browser extension)

## Configuration

The configuration for TalkXTyper is stored in a JSON file located in your user
configuration directory. The file is named `talkxtyper-config.json`.

### Configuration Options

- `OpenAIKey`: Your API key for the OpenAI Whisper API.
- `IncludeScreen`: A boolean value indicating whether to analyze the screen to augment the transcription. The config file will be updated automatically if you change this value in the program.
- `IncludeNvim`: A boolean value indicating whether to analyze the screen to augment the transcription.

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

