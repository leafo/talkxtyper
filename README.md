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
   My first attempt was to take a screenshot of the desktop while the audio was
   recording and send that to GPT-4 to ask it to extract relevant textual
   features from the image. The result would be sent alongside the prompt to
   another call to ChatGPT to fix up the transcription to try to rewrite things
   to match the user's intent. I considered this a failed attempt because the
   initial ChatGPT call with the screenshot took much longer than the recording
   and processing by the Whisper API, so you would end up waiting a bit for the
   result. The returned words were sparse and may not have been relevant to the
   user.

2. **Using the `prompt` parameter with Whisper API**
   The prompt parameter produces poor results. According to the documentation,
   it has limited support for controlling how the transcription and support a
   relatively small max size. If any fixups need to take place, they will need
   to be done with a subsequent call to ChatGPT. Luckily, GPT-4 is fast enough
   that the delay does not ruin the experience.

3. **Extract text from running app**
   My next attempt was to extract the textual context from the running app. To
   start with, I used the `nvim` remote API to run commands on my nvim session to
   pull out the text currently being edited.

## Configuration

The configuration for TalkXTyper is stored in a JSON file located in your user
configuration directory. The file is named `talkxtyper-config.json`.

### Configuration Options

- `OpenAIKey`: Your API key for the OpenAI Whisper API.
- `IncludeScreen`: A boolean value indicating whether to analyze the screen to augment the transcription. The config file will be updated automatically if you change this value in the program.
- `IncludeNvim`: A boolean value indicating whether to analyze the screen to augment the transcription.

## Installation

To install TalkXTyper, you will need to have Go installed. Run the following command:

    go install github.com/leafo/talkxtyper@latest

This project has only been tested on Linux, but it uses cross-platform libraries, so it should work on other platforms.

## License

This project is licensed under the MIT License. See the LICENSE file for details.

