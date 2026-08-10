package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

var fixPrompt = `You are a voice-to-text typing program that takes the textual result of an automated transcription and a context from the user's current environment (e.g. their active editor buffer, terminal pane, or screen) and fixes the transcription to be what the user likely intended to type.

You will output only the updated transcription and no other text. Do not output information not spoken in the original transcription. If the transcription is already correct or the context isn't relevant, return the transcription unchanged.`

// tests:
// The transcription was generated from spoken words and may contain errors. Please use the text provided to identify and correct any inaccuracies, focusing on misheard words, technical terms, or any context-specific discrepancies.

func getOpenAIClient() (*openai.Client, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = config.OpenAIKey
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &client, nil
}

// gpt-transcribe receives the available application context during transcription.
// The second pass remains as a final correction step for identifiers and other
// context-specific text.
func transcribeAudio(ctx context.Context, mp3FilePath string, instructions string) (*TranscriptionResult, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return nil, fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	audioFile, err := os.Open(mp3FilePath)
	if err != nil {
		return nil, fmt.Errorf("Error opening audio file: %v", err)
	}
	defer audioFile.Close()

	req := openai.AudioTranscriptionNewParams{
		File:      audioFile,
		Model:     "gpt-transcribe",
		Languages: []string{"en"},
	}
	if instructions != "" {
		req.Prompt = openai.String(instructions)
	}

	resp, err := client.Audio.Transcriptions.New(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Error sending transcription request: %v", err)
	}

	result := NewTranscriptionResult()

	result.Original = resp.Text

	if instructions != "" {
		result.RepairPrompt = instructions
		result.RepairModel = config.ChatModel
		fixedText, err := fixTranscription(ctx, resp.Text, instructions)
		if err != nil {
			return nil, fmt.Errorf("Error fixing transcription: %v", err)
		}
		result.Modified = fixedText
	}

	return result, nil
}

func fixTranscription(ctx context.Context, transcribedText string, instructions string) (string, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return "", fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	req := responses.ResponseNewParams{
		Model:           config.ChatModel,
		Instructions:    openai.String(fixPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(fmt.Sprintf("Context: %s\n\nTranscription: %s", instructions, transcribedText))},
		MaxOutputTokens: openai.Int(1024),
		Store:           openai.Bool(false),
	}

	resp, err := client.Responses.New(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending transcription fix request: %v", err)
	}

	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("Transcription fix returned no text")
	}
	return text, nil
}

func describeImage(ctx context.Context, imagePath string) (string, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return "", fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("Error reading image file: %v", err)
	}

	// Encode image to base64
	encodedImage := base64.StdEncoding.EncodeToString(imageData)
	imageDataURL := fmt.Sprintf("data:image/png;base64,%s", encodedImage)

	imageInput := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
	imageInput.OfInputImage.ImageURL = openai.String(imageDataURL)

	req := responses.ResponseNewParams{
		Model:        config.ChatModel,
		Instructions: openai.String("You are a voice to text typing assistant who is collecting text on the user's current screen so that a machine generated transcription can be edited to match any phrases appearing on the screen. Include 1 sentence description of what the user is engaging with. Then list out all relevant keywords/names/words that appear in the provided image so that the transcription may be corrected."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(
					responses.ResponseInputMessageContentListParam{imageInput},
					responses.EasyInputMessageRoleUser,
				),
			},
		},
		Store: openai.Bool(false),
	}

	resp, err := client.Responses.New(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending image description request: %v", err)
	}

	text := resp.OutputText()
	if text == "" {
		return "", fmt.Errorf("Image description returned no text")
	}
	return text, nil
}
