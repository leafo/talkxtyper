package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

var fixPrompt = `You are a voice-to-text typing program that takes the textual result of an automated transcription and a context from the user's current environment (e.g. their active editor buffer, terminal pane, or screen) and fixes the transcription to be what the user likely intended to type.

You will output only the updated transcription and no other text. Do not output information not spoken in the original transcription. If the transcription is already correct or the context isn't relevant, return the transcription unchanged.`

// tests:
// The transcription was generated from spoken words and may contain errors. Please use the text provided to identify and correct any inaccuracies, focusing on misheard words, technical terms, or any context-specific discrepancies.

func getOpenAIClient() (*openai.Client, error) {
	apiKey, err := getOpenAIAPIKey()
	if err != nil {
		return nil, err
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &client, nil
}

func getOpenAIAPIKey() (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = config.OpenAIKey
		if apiKey == "" {
			return "", fmt.Errorf("OpenAI API key is not set")
		}
	}
	return apiKey, nil
}

// gpt-transcribe receives the available application context during transcription.
// The second pass remains as a final correction step for identifiers and other
// context-specific text.
func transcribeOpenAIAudio(ctx context.Context, mp3FilePath string, transcriptionContext TranscriptionContext) (*TranscriptionResult, error) {
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
		Keywords:  transcriptionContext.Keywords,
		Languages: transcriptionContext.Languages,
	}
	if transcriptionContext.Prompt != "" {
		req.Prompt = openai.String(transcriptionContext.Prompt)
	}

	transcribeStart := time.Now()
	resp, err := client.Audio.Transcriptions.New(ctx, req)
	transcribeElapsed := time.Since(transcribeStart)
	log.Printf("Transcription (%s) took %s\n", req.Model, transcribeElapsed)
	if err != nil {
		return nil, fmt.Errorf("Error sending transcription request: %v", err)
	}

	result := NewTranscriptionResult()
	result.Original = resp.Text
	result.TranscriptionProvider = TranscriptionProviderOpenAI
	result.TranscriptionMode = TranscriptionModeBuffered
	result.TranscriptionModel = "gpt-transcribe"
	result.TranscriptionElapsed = transcribeElapsed
	result.TranscriptionKeywords = transcriptionContext.Keywords

	if transcriptionContext.Prompt != "" {
		result.RepairPrompt = transcriptionContext.Prompt
		result.RepairModel = config.ChatModel
		fixedText, repairElapsed, err := fixTranscription(ctx, resp.Text, transcriptionContext.Prompt)
		if err != nil {
			return nil, fmt.Errorf("Error fixing transcription: %v", err)
		}
		result.Modified = fixedText
		result.RepairElapsed = repairElapsed
	}

	return result, nil
}

func fixTranscription(ctx context.Context, transcribedText string, instructions string) (string, time.Duration, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return "", 0, fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	req := responses.ResponseNewParams{
		Model:           config.ChatModel,
		Instructions:    openai.String(fixPrompt),
		Input:           responses.ResponseNewParamsInputUnion{OfString: openai.String(fmt.Sprintf("Context: %s\n\nTranscription: %s", instructions, transcribedText))},
		MaxOutputTokens: openai.Int(1024),
		Store:           openai.Bool(false),
	}

	repairStart := time.Now()
	resp, err := client.Responses.New(ctx, req)
	repairElapsed := time.Since(repairStart)
	log.Printf("Repair (%s) took %s\n", req.Model, repairElapsed)
	if err != nil {
		return "", repairElapsed, fmt.Errorf("Error sending transcription fix request: %v", err)
	}

	text := resp.OutputText()
	if text == "" {
		return "", repairElapsed, fmt.Errorf("Transcription fix returned no text")
	}
	return text, repairElapsed, nil
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
		Instructions: openai.String("You are a voice to text typing assistant collecting context from the user's screen to improve transcription. Write one sentence describing what the user is doing. Then write a heading exactly `Keywords:` followed by one relevant literal name, identifier, command, path, acronym, or short technical phrase per line, each prefixed with `- `. Include only terms that visibly appear in the image."),
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
