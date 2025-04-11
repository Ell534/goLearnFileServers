package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)

func getVideoAspectRatio(filePath string) (string, error) {
	ffprobeCmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var cmdOutput bytes.Buffer
	ffprobeCmd.Stdout = &cmdOutput
	err := ffprobeCmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running ffprobeCmd: %v", err)
	}

	type Stream struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}

	type FFProbeOutput struct {
		Streams []Stream `json:"streams"`
	}

	var output FFProbeOutput
	err = json.Unmarshal(cmdOutput.Bytes(), &output)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling json: %v", err)
	}

	if len(output.Streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	width := output.Streams[0].Width
	height := output.Streams[0].Height
	ratio := float64(width) / float64(height)

	const (
		ratio16by9 = 16.0 / 9.0
		ratio9by16 = 9.0 / 16.0
		tolerance  = 0.1
	)

	if math.Abs(ratio-ratio16by9) <= tolerance {
		return "landscape", nil
	} else if math.Abs(ratio-ratio9by16) <= tolerance {
		return "portrait", nil
	} else {
		return "other", nil
	}
}
