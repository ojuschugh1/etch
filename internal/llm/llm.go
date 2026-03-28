package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ojuschugh1/etch/internal/config"
)

// LLMClient sends diff content to a configured LLM endpoint for summarization.
type LLMClient struct {
	Endpoint string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

// NewLLMClient creates an LLMClient from config. Returns nil if not configured.
func NewLLMClient(cfg *config.LLMConfig) *LLMClient {
	if cfg == nil || cfg.APIKey == "" {
		return nil
	}
	return &LLMClient{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
		Model:    cfg.Model,
		Timeout:  10 * time.Second,
	}
}

// chatRequest is the OpenAI-compatible chat completions request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// chatMessage represents a single message in the chat completions API.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the OpenAI-compatible chat completions response body.
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

// chatChoice represents a single choice in the chat completions response.
type chatChoice struct {
	Message chatMessage `json:"message"`
}

// Summarize sends diff content to the LLM and returns a plain-English summary.
func (c *LLMClient) Summarize(diffContent string) (string, error) {
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a helpful assistant that summarizes API response diffs in plain English."},
			{Role: "user", Content: diffContent},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling LLM request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating LLM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{Timeout: c.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading LLM response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parsing LLM response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("LLM response contained no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}
