package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("1.18.4")
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fail("unsupported invocation")
	}
	if os.Getenv("OPENCODE_PERMISSION") == "" || os.Getenv("OPENCODE_CONFIG_DIR") == "" || os.Getenv("OPENCODE_DISABLE_AUTOUPDATE") != "true" || os.Getenv("OPENCODE_YOLO") != "false" {
		fail("missing fail-closed runtime environment")
	}
	var configuration struct {
		Agent map[string]struct {
			Prompt string `json:"prompt"`
		} `json:"agent"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &configuration); err != nil || len(configuration.Agent) != 1 {
		fail("invalid ephemeral configuration")
	}
	var promptText string
	for _, agent := range configuration.Agent {
		promptText = agent.Prompt
	}
	var prompt struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
		Work struct {
			TaskID string `json:"taskId"`
		} `json:"work"`
		Output struct {
			Template map[string]any `json:"template"`
		} `json:"output"`
	}
	if err := json.Unmarshal([]byte(promptText), &prompt); err != nil || prompt.Agent.ID == "" || prompt.Work.TaskID == "" || prompt.Output.Template == nil {
		fail("invalid prompt bundle")
	}
	result := prompt.Output.Template
	if result["taskId"] != prompt.Work.TaskID || result["agentId"] != prompt.Agent.ID {
		fail("result identity template drift")
	}
	result["summary"] = "hermetic dispatch completed"
	result["nextRecommended"] = "inspect Chronicle evidence"
	resultData, err := json.Marshal(result)
	if err != nil {
		fail("encode result")
	}
	event := map[string]any{
		"type": "text", "timestamp": 1, "sessionID": "hermetic-session",
		"part": map[string]any{"id": "hermetic-result", "type": "text", "text": string(resultData)},
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(event); err != nil {
		fail("encode event")
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(message))
	os.Exit(2)
}
