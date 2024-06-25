package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/sashabaranov/go-openai"
)

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

func transcribeAudio(ctx context.Context, mp3FilePath string, description string) (string, error) {
	client, err := getOpenAIClient()
	if err != nil {
		return "", fmt.Errorf("Error initializing OpenAI client: %v", err)
	}

	// Create a request for transcription
	req := openai.AudioRequest{
		FilePath:    mp3FilePath,
		Model:       "whisper-1",
		Language:    "en",
		Temperature: 0.5,
		Prompt:      description,
	}

	// Perform the transcription
	resp, err := client.CreateTranscription(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending transcription request: %v", err)
	}

	return resp.Text, nil
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

	log.Printf("Request: %+v\n", req)

	// Perform the image description
	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("Error sending image description request: %v", err)
	}

	return resp.Choices[0].Message.Content, nil
}
