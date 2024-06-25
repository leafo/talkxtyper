package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

type Config struct {
	OpenAIKey     string
	IncludeScreen bool
	IncludeNvim   bool
	ListenAddress string
}

var config = Config{
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
