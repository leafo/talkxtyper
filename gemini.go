package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/genai"
)

const geminiTranscriptionModel = "gemini-3.5-transcribe"

func getGeminiAPIKey() (string, error) {
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		return apiKey, nil
	}
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		return apiKey, nil
	}
	if config.GeminiKey != "" {
		return config.GeminiKey, nil
	}
	return "", fmt.Errorf("Gemini API key is not set")
}

func getGeminiClient(ctx context.Context) (*genai.Client, error) {
	apiKey, err := getGeminiAPIKey()
	if err != nil {
		return nil, err
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{
			APIVersion: "v1beta",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initializing Gemini client: %w", err)
	}
	return client, nil
}

func geminiLanguageCodes(languages []string) []string {
	codes := make([]string, 0, len(languages))
	for _, language := range languages {
		switch language {
		case "", "auto":
			continue
		case "en":
			codes = append(codes, "en-US")
		default:
			codes = append(codes, language)
		}
	}
	return codes
}

func geminiAudioTranscriptionConfig(transcriptionContext TranscriptionContext) *genai.AudioTranscriptionConfig {
	return &genai.AudioTranscriptionConfig{
		LanguageCodes:    geminiLanguageCodes(transcriptionContext.Languages),
		CustomVocabulary: transcriptionContext.Keywords,
		Mode:             genai.AudioTranscriptionConfigModeVerbatim,
	}
}

func transcribeGeminiAudio(ctx context.Context, mp3FilePath string, transcriptionContext TranscriptionContext) (*TranscriptionResult, error) {
	client, err := getGeminiClient(ctx)
	if err != nil {
		return nil, err
	}

	mp3Bytes, err := os.ReadFile(mp3FilePath)
	if err != nil {
		return nil, fmt.Errorf("reading audio for Gemini: %w", err)
	}

	request := []*genai.Content{genai.NewContentFromParts(
		[]*genai.Part{genai.NewPartFromBytes(mp3Bytes, "audio/mp3")},
		genai.RoleUser,
	)}
	requestConfig := &genai.GenerateContentConfig{
		AudioTranscriptionConfig: geminiAudioTranscriptionConfig(transcriptionContext),
	}

	transcribeStart := time.Now()
	response, err := client.Models.GenerateContent(ctx, geminiTranscriptionModel, request, requestConfig)
	transcribeElapsed := time.Since(transcribeStart)
	log.Printf("Transcription (%s) took %s\n", geminiTranscriptionModel, transcribeElapsed)
	if err != nil {
		return nil, fmt.Errorf("sending Gemini transcription request: %w", err)
	}
	text := response.Text()
	if text == "" {
		return nil, fmt.Errorf("Gemini transcription returned no text")
	}

	result := NewTranscriptionResult()
	result.Original = text
	result.TranscriptionProvider = TranscriptionProviderGemini
	result.TranscriptionMode = TranscriptionModeBuffered
	result.TranscriptionModel = geminiTranscriptionModel
	result.TranscriptionElapsed = transcribeElapsed
	result.TranscriptionKeywords = transcriptionContext.Keywords
	result.TranscriptionLanguages = transcriptionContext.Languages
	result.ContextPrompt = transcriptionContext.Prompt

	// The repair model remains independent from the transcription provider. This
	// preserves the existing context-aware correction behavior for buffered mode.
	if transcriptionContext.Prompt != "" {
		result.RepairModel = config.ChatModel
		fixedText, repairElapsed, err := fixTranscription(ctx, text, transcriptionContext.Prompt)
		if err != nil {
			return nil, fmt.Errorf("fixing transcription: %w", err)
		}
		result.Modified = fixedText
		result.RepairElapsed = repairElapsed
	}

	return result, nil
}
