package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/sashabaranov/go-openai"
)

type TranscriptionResult struct {
	Original string
	Modified string
}

func (tr *TranscriptionResult) String() string {
	if tr.Modified != "" {
		return tr.Modified
	}
	return tr.Original
}

func (tr *TranscriptionResult) IsEmpty() bool {
	return tr.Original == "" && tr.Modified == ""
}

func getOpenAIClient() (*openai.Client, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = config.OpenAIKey
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set")
		}
	}
	return openai.NewClient(apiKey), nil
}

// in my testing the Prompt parameter is not very good at repairing the transcription, so we do a two pass process instead
func transcribeAudio(ctx context.Context, mp3FilePath string, instructions string) (TranscriptionResult, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	// Create a request for transcription
	req := openai.AudioRequest{
		FilePath:    mp3FilePath,
		Model:       "whisper-1",
		Language:    "en",
		Temperature: 0.5,
		// Prompt:      instructions,
	}

	// Perform the transcription
	resp, err := client.CreateTranscription(ctx, req)
	if err != nil {
		return TranscriptionResult{}, fmt.Errorf("Error sending transcription request: %v", err)
	}

	result := TranscriptionResult{Original: resp.Text}

	if instructions != "" {
		fixedText, err := fixTranscription(ctx, resp.Text, instructions)
		if err != nil {
			return TranscriptionResult{}, fmt.Errorf("Error fixing transcription: %v", err)
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

	var messages = []openai.ChatCompletionMessage{
		{
			Role:    "system",
			Content: "You are an voice-to-text typing program that takes the textual result of an automated transcription and a context from the user's screen and fixes the transcription to be what the user likely intended to type. You will output only the updated transcription and no other text.",
		},
		{
			Role:    "user",
			Content: instructions,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Transcription: %s", transcribedText),
		},
	}

	req := openai.ChatCompletionRequest{
		Model:     "gpt-4o",
		Messages:  messages,
		MaxTokens: 1024,
	}

	// log.Printf("ChatCompletion for fixing transcription: %+v\n", req)

	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending transcription fix request: %v", err)
	}

	return resp.Choices[0].Message.Content, nil
}

func describeImage(ctx context.Context, imagePath string) (string, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return "", fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	// Read image file
	imageData, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("Error reading image file: %v", err)
	}

	// Encode image to base64
	encodedImage := base64.StdEncoding.EncodeToString(imageData)
	imageDataURL := fmt.Sprintf("data:image/png;base64,%s", encodedImage)

	var imageMessage = openai.ChatCompletionMessage{
		Role: "user",
		MultiContent: []openai.ChatMessagePart{
			{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL: imageDataURL,
				},
			},
		},
	}

	var messages = []openai.ChatCompletionMessage{
		openai.ChatCompletionMessage{
			Role:    "system",
			Content: "You are a voice to text typing assistant who is collecting text on the user's current screen so that a machine generated transcription can be edited to match any phrases appearing on the screen. Include 1 sentence description of what the user is engaging with. Then list out all relevant keywords/names/words that appear in the provided image so that the transcription may be corrected.",
		},
		imageMessage,
	}

	// Create a request for image description
	req := openai.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	}

	// log.Printf("ChatCompletion: %+v\n", req)

	// Perform the image description
	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending image description request: %v", err)
	}

	return resp.Choices[0].Message.Content, nil
}
