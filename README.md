# TalkXTyper

TalkXTyper is a desktop application that will, on command, record your voice,
transcribe it using the OpenAI Whisper API, and "type" it to your computer. It
is activated with a global hotkey so that you do not lose focus on the area
you're typing into. It can also take a screenshot of your screen and extract
textual information to send along to the transcription call to help match words
or phrases used on the screen.

## Configuration

The configuration for TalkXTyper is stored in a JSON file located in your user
configuration directory. The file is named `talkxtyper-config.json`.

### Configuration Options

- `OpenAIKey`: Your API key for the OpenAI Whisper API.
- `IncludeScreen`: A boolean value indicating whether to analyze the screen to augment the transcription. The config file will be updated automatically if you change this value in the program.

## Installation

To install TalkXTyper, you will need to have Go installed. Run the following command:

    go install github.com/leafo/talkxtyper@latest

This project has only been tested on Linux, but it uses cross-platform libraries, so it should work on other platforms.

## License

This project is licensed under the MIT License. See the LICENSE file for details.

