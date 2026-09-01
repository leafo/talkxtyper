package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

type TranscriptionMode string
type TranscriptionProvider string

const (
	TranscriptionModeBuffered TranscriptionMode = "buffered"
	TranscriptionModeLive     TranscriptionMode = "live"

	TranscriptionProviderOpenAI TranscriptionProvider = "openai"
	TranscriptionProviderGemini TranscriptionProvider = "gemini"
)

func normalizeTranscriptionMode(mode TranscriptionMode) TranscriptionMode {
	if mode == TranscriptionModeLive {
		return TranscriptionModeLive
	}
	return TranscriptionModeBuffered
}

func normalizeTranscriptionProvider(provider TranscriptionProvider) TranscriptionProvider {
	if provider == TranscriptionProviderGemini {
		return TranscriptionProviderGemini
	}
	return TranscriptionProviderOpenAI
}

type Config struct {
	OpenAIKey             string
	GeminiKey             string
	ChatModel             string
	TranscriptionProvider TranscriptionProvider
	TranscriptionMode     TranscriptionMode
	IncludeScreen         bool
	IncludeNvim           bool
	IncludeTmux           bool
	ListenAddress         string
	// GeminiSmartMode asks Gemini transcription to remove filler words, false
	// starts and repetitions and to apply light formatting, instead of
	// transcribing verbatim.
	GeminiSmartMode bool
	// Keywords are always sent as transcription hints, ahead of any terms
	// extracted from the collected context.
	Keywords []string
}

var config = Config{
	ChatModel:             "gpt-5.6-luna",
	TranscriptionProvider: TranscriptionProviderOpenAI,
	TranscriptionMode:     TranscriptionModeBuffered,
	// ListenAddress: "localhost:9898",
	// IncludeScreen: true,
	// IncludeNvim: true,
}

func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("Error finding user config directory: %v", err)
	}
	return fmt.Sprintf("%s/talkxtyper-config.json", configDir), nil
}

func readConfig() error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("Error getting config path: %v", err)
	}
	configFile, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("Error opening config file: %v", err)
	}
	defer configFile.Close()

	byteValue, err := ioutil.ReadAll(configFile)
	if err != nil {
		return fmt.Errorf("Error reading config file: %v", err)
	}

	if err := json.Unmarshal(byteValue, &config); err != nil {
		return fmt.Errorf("Error unmarshalling config file: %v", err)
	}
	config.TranscriptionMode = normalizeTranscriptionMode(config.TranscriptionMode)
	config.TranscriptionProvider = normalizeTranscriptionProvider(config.TranscriptionProvider)

	log.Printf("Configuration loaded: %s\n", configPath)

	return nil
}

func writeConfig() error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("Error getting config path: %v", err)
	}
	configFile, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("Error creating config file: %v", err)
	}
	defer configFile.Close()

	byteValue, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("Error marshalling config to JSON: %v", err)
	}

	if _, err := configFile.Write(byteValue); err != nil {
		return fmt.Errorf("Error writing to config file: %v", err)
	}

	log.Printf("Config file has been written: %s\n", configPath)

	return nil
}
