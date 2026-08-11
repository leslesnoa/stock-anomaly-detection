package gateway

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const claudeMaxTokens = 300

type ClaudeClient struct {
	client anthropic.Client
	model  string
}

func NewClaudeClient(apiKey, model string) *ClaudeClient {
	return &ClaudeClient{
		// SDK側の自動リトライ（デフォルト2回）を無効化し、GenerateReportの
		// 「1回だけリトライする」という仕様どおりの回数でリクエストする。
		client: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithMaxRetries(0)),
		model:  model,
	}
}

func NewClaudeClientWithBaseURL(apiKey, baseURL, model string) *ClaudeClient {
	return &ClaudeClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithMaxRetries(0)),
		model:  model,
	}
}

// GenerateReport はClaude APIでAI分析レポートを生成する。
// 失敗時は仕様どおり1回だけリトライする。
func (c *ClaudeClient) GenerateReport(prompt string) (string, error) {
	report, err := c.generate(prompt)
	if err != nil {
		report, err = c.generate(prompt)
	}
	if err != nil {
		return "", fmt.Errorf("claude api: %w", err)
	}
	return report, nil
}

func (c *ClaudeClient) generate(prompt string) (string, error) {
	message, err := c.client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: claudeMaxTokens,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", err
	}
	if message.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("claude refused the request")
	}
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			return text.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in claude response")
}
