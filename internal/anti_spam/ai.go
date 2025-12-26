package anti_spam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/artalkjs/artalk/v2/internal/log"
)

var _ Checker = (*AIChecker)(nil)

type AIChecker struct {
	apiKey string
	model  string
	host   string
}

func NewAIChecker(apiKey, model, host string) Checker {
	if host == "" {
		host = "api.openai.com"
	}
	// Remove trailing slash and protocol if present
	host = strings.TrimSuffix(host, "/")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")

	return &AIChecker{
		apiKey: apiKey,
		model:  model,
		host:   host,
	}
}

func (*AIChecker) Name() string {
	return "ai"
}

func (c *AIChecker) Check(p *CheckerParams) (bool, error) {
	prompt := buildModerationPrompt(p)

	response, err := c.callAPI(prompt)
	if err != nil {
		return false, err
	}

	log.Debug(LOG_TAG, "[AI] Moderation response: ", response)

	return parseAIResponse(response), nil
}

func buildModerationPrompt(p *CheckerParams) string {
	return fmt.Sprintf(`You are a content moderation assistant. Your task is to determine if the following comment should be approved or blocked.

A comment should be BLOCKED if it contains:
- Spam or advertising
- Hate speech or discrimination
- Harassment or personal attacks
- Pornographic or sexually explicit content
- Violence or threats
- Illegal content
- Meaningless or gibberish text
- Excessive profanity

Comment Information:
- Author: %s
- Email: %s
- Content: %s

Respond with ONLY one word: "PASS" if the comment should be approved, or "BLOCK" if it should be blocked.`, p.UserName, p.UserEmail, p.Content)
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AIChecker) callAPI(prompt string) (string, error) {
	reqBody := openAIRequest{
		Model: c.model,
		Messages: []openAIMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := fmt.Sprintf("https://%s/v1/chat/completions", c.host)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call AI API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if openAIResp.Error != nil {
		return "", fmt.Errorf("AI API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("no response from AI API")
	}

	return openAIResp.Choices[0].Message.Content, nil
}

func parseAIResponse(response string) bool {
	response = strings.TrimSpace(strings.ToUpper(response))

	// If response contains "PASS", consider it passed
	if strings.Contains(response, "PASS") {
		return true
	}

	// If response contains "BLOCK", consider it blocked
	if strings.Contains(response, "BLOCK") {
		return false
	}

	// Default to pass if the response is unclear
	log.Warn(LOG_TAG, "[AI] Unclear response, defaulting to pass: ", response)
	return true
}
