package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

const systemPrompt = `You are a senior engineer making the case for one technical option. You argue from first principles. You acknowledge valid counterarguments. You update your position when presented with compelling evidence. You do not repeat yourself. Each turn must add new reasoning or concede a specific point. Keep responses under 300 words.`

func getLLMModel() string {
	if m := os.Getenv("LLM_MODEL"); m != "" {
		return m
	}
	return "deepseek/deepseek-chat" // works on both OpenRouter and LM Studio (LM Studio ignores this)
}

func callLLM(apiKey, baseURL string, messages []map[string]string) (string, int, int, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      getLLMModel(),
		"max_tokens": 600,
		"temperature": 0.7,
		"messages":   messages,
	})
	req, _ := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("openrouter: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", 0, 0, fmt.Errorf("openrouter %d: %s", resp.StatusCode, string(data))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(data, &result)
	if len(result.Choices) == 0 {
		return "", 0, 0, fmt.Errorf("no choices")
	}
	return result.Choices[0].Message.Content, result.Usage.PromptTokens, result.Usage.CompletionTokens, nil
}

func main() {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		apiKey = "lm-studio"
	}
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		resp := Response{JSONRPC: "2.0", ID: req.ID}

		switch req.Method {
		case "ping":
			resp.Result = map[string]string{"status": "ok", "agent": "adr-advocate"}

		case "argue":
			var params map[string]any
			json.Unmarshal(req.Params, &params)
			question, _ := params["question"].(string)
			opponentSaid, _ := params["opponent_said"].(string)

			messages := []map[string]string{{"role": "system", "content": systemPrompt}}

			if opponentSaid == "" {
				messages = append(messages, map[string]string{
					"role":    "user",
					"content": fmt.Sprintf("Make the case for the best option for this decision:\n\n%s", question),
				})
			} else {
				messages = append(messages, map[string]string{
					"role":    "user",
					"content": fmt.Sprintf("The question: %s\n\nThe skeptic responded:\n\n%s\n\nRespond to their specific points. Add new reasoning or concede where they're right.", question, opponentSaid),
				})
			}

			content, tokIn, tokOut, err := callLLM(apiKey, baseURL, messages)
			if err != nil {
				resp.Error = map[string]any{"code": -32000, "message": err.Error()}
			} else {
				resp.Result = map[string]any{
					"argument":    content,
					"tokens_in":   tokIn,
					"tokens_out":  tokOut,
				}
			}

		default:
			resp.Error = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
		}

		json.NewEncoder(os.Stdout).Encode(resp)
	}
}
