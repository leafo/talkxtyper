package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/go-vgo/robotgo"
)

// capture screen and ask openai to describe it, cleans up any temp files
func describeScreen(ctx context.Context) (string, error) {
	// Take a screenshot
	path, err := takeScreenshot()
	if err != nil {
		return "", fmt.Errorf("Error taking screenshot: %v", err)
	}
	defer os.Remove(path)

	// Describe the image
	description, err := describeImage(ctx, path)
	if err != nil {
		return "", fmt.Errorf("Error describing image: %v", err)
	}

	return description, nil
}

// capture a screenshot of the display and save it to a temporary file
func takeScreenshot() (string, error) {
	image := robotgo.CaptureImg()

	tempFile, err := ioutil.TempFile("", fmt.Sprintf("talkxtyper-%d-*.png", time.Now().Unix()))
	if err != nil {
		return "", fmt.Errorf("Error creating temporary file: %v", err)
	}
	defer tempFile.Close()

	// Save the screenshot to the temporary file
	err = robotgo.Save(image, tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("Error saving screenshot: %v", err)
	}

	return tempFile.Name(), nil
}
