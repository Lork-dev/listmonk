package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

type aiGenerateReq struct {
	Prompt string `json:"prompt"`
}

type aiGenerateResp struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type deepseekReq struct {
	Model    string            `json:"model"`
	Messages []deepseekMessage `json:"messages"`
}

type deepseekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepseekResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const aiSystemPrompt = `Ты помогаешь писать email-рассылки для сервиса Lork — конструктора сайтов с AI.
Пользователь описывает суть письма. Ты генерируешь:
1. Тему письма (subject) — короткую, цепляющую, без кликбейта
2. HTML-тело письма — красиво оформленное, с заголовком, абзацами и CTA-кнопкой

Правила:
- Стиль: дружелюбный, профессиональный, на русском языке
- HTML должен быть чистым, без <html>/<head>/<body> тегов — только контент
- Используй inline-стили для оформления (max-width 600px, шрифты, цвета)
- CTA-кнопка: синяя (#2563EB), скруглённая, ссылка https://lork.dev
- Переменные шаблона listmonk: {{ .Subscriber.FirstName }} для имени

Верни строго JSON без markdown-обёртки:
{"subject":"...","body":"..."}`

// AIGenerate calls DeepSeek to generate an email subject and HTML body.
func (a *App) AIGenerate(c echo.Context) error {
	var req aiGenerateReq
	if err := c.Bind(&req); err != nil || req.Prompt == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "prompt is required")
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "DEEPSEEK_API_KEY not configured")
	}

	payload := deepseekReq{
		Model: "deepseek-chat",
		Messages: []deepseekMessage{
			{Role: "system", Content: aiSystemPrompt},
			{Role: "user", Content: req.Prompt},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to build request")
	}

	httpReq, err := http.NewRequestWithContext(c.Request().Context(),
		http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "DeepSeek API unreachable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("DeepSeek error %d: %s", resp.StatusCode, string(b)))
	}

	var dsResp deepseekResp
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil || len(dsResp.Choices) == 0 {
		return echo.NewHTTPError(http.StatusBadGateway, "invalid DeepSeek response")
	}

	content := dsResp.Choices[0].Message.Content

	var result aiGenerateResp
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Fallback: вернём сырой текст как body
		result.Subject = "Новое письмо"
		result.Body = content
	}

	return c.JSON(http.StatusOK, result)
}
