package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

var mcpURL = "http://127.0.0.1:9876"
var mcpAPIKey = ""

func init() {
	if u := os.Getenv("MCP_META_URL"); u != "" {
		mcpURL = u
	}
	if k := os.Getenv("AUTH_FREE_KEY"); k != "" {
		mcpAPIKey = k
	} else if k := os.Getenv("MCP_META_API_KEY"); k != "" {
		mcpAPIKey = k
	}
}

type Turn struct {
	Agent   string `json:"agent"`
	Round   int    `json:"round"`
	Content string `json:"content"`
}

// emitProgress writes a JSON-RPC notification (no id) to stdout.
// The supervisor detects these and forwards them as SSE progress events.
func emitProgress(agent string, round int, content string) {
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "progress",
		"params": map[string]any{
			"agent":   agent,
			"round":   round,
			"content": content,
		},
	}
	json.NewEncoder(os.Stdout).Encode(notif)
}

type Cost struct {
	TotalTokensIn  int     `json:"total_tokens_in"`
	TotalTokensOut int     `json:"total_tokens_out"`
	EstimatedCost  float64 `json:"estimated_cost_usd"`
}

func callPod(tool string, args map[string]any) (map[string]any, error) {
	payload, _ := json.Marshal(map[string]any{"tool": tool, "arguments": args})
	req, err := http.NewRequest("POST", mcpURL+"/v1/execute", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", tool, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if mcpAPIKey != "" {
		req.Header.Set("X-API-Key", mcpAPIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", tool, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var result struct {
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	json.Unmarshal(data, &result)
	if result.Error != "" {
		return nil, fmt.Errorf("call %s: %s", tool, result.Error)
	}
	return result.Result, nil
}

func hasConcession(text string) bool {
	lower := strings.ToLower(text)
	markers := []string{
		"i concede", "you're right", "you are right",
		"fair point, i agree", "i must agree", "i have to agree",
		"the advocate makes a compelling case", "the advocate is correct",
		"i cannot disagree", "i stand corrected",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func runDebate(question string) map[string]any {
	var transcript []Turn
	var cost Cost
	maxRounds := 3
	concessionDetected := false

	addCost := func(result map[string]any) {
		if tokIn, ok := result["tokens_in"].(float64); ok {
			cost.TotalTokensIn += int(tokIn)
		}
		if tokOut, ok := result["tokens_out"].(float64); ok {
			cost.TotalTokensOut += int(tokOut)
		}
	}

	for round := 1; round <= maxRounds; round++ {
		// Advocate argues
		advocateArgs := map[string]any{"question": question}
		if round > 1 {
			// Pass the skeptic's last response
			lastSkeptic := ""
			for i := len(transcript) - 1; i >= 0; i-- {
				if transcript[i].Agent == "skeptic" {
					lastSkeptic = transcript[i].Content
					break
				}
			}
			advocateArgs["opponent_said"] = lastSkeptic
		}

		advocateResult, err := callPod("adr-advocate.argue", advocateArgs)
		if err != nil {
			return map[string]any{"error": fmt.Sprintf("advocate round %d: %v", round, err)}
		}

		advocateArg, _ := advocateResult["argument"].(string)
		addCost(advocateResult)
		transcript = append(transcript, Turn{Agent: "advocate", Round: round, Content: advocateArg})
		emitProgress("advocate", round, advocateArg)

		// Skeptic challenges
		skepticArgs := map[string]any{"proposal": advocateArg}
		if round > 1 {
			skepticArgs["opponent_said"] = advocateArg
		}

		skepticResult, err := callPod("adr-skeptic.challenge", skepticArgs)
		if err != nil {
			return map[string]any{"error": fmt.Sprintf("skeptic round %d: %v", round, err)}
		}

		skepticArg, _ := skepticResult["argument"].(string)
		addCost(skepticResult)
		transcript = append(transcript, Turn{Agent: "skeptic", Round: round, Content: skepticArg})
		emitProgress("skeptic", round, skepticArg)

		// Check for concession
		if hasConcession(skepticArg) || hasConcession(advocateArg) {
			concessionDetected = true
			break
		}
	}

	// Build transcript text for decision maker
	var transcriptText strings.Builder
	for _, t := range transcript {
		transcriptText.WriteString(fmt.Sprintf("[%s, round %d]\n%s\n\n", strings.ToUpper(t.Agent), t.Round, t.Content))
	}

	// Decision maker
	decisionResult, err := callPod("adr-decision-maker.decide", map[string]any{
		"question":   question,
		"transcript": transcriptText.String(),
	})
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("decision maker: %v", err)}
	}

	decision, _ := decisionResult["decision"].(string)
	addCost(decisionResult)
	emitProgress("decision-maker", 0, decision)

	// Writer
	writerResult, err := callPod("adr-writer.write", map[string]any{
		"question":   question,
		"transcript": transcriptText.String(),
		"decision":   decision,
	})
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("writer: %v", err)}
	}

	adr, _ := writerResult["adr"].(string)
	addCost(writerResult)

	// Ingest decision into Hebbian knowledge graph
	callPod("hebbian.ingest", map[string]any{
		"question": question,
		"answer":   decision,
	})

	// Cost estimate (deepseek pricing: ~$0.14/M input, ~$0.28/M output)
	cost.EstimatedCost = float64(cost.TotalTokensIn)*0.00000014 + float64(cost.TotalTokensOut)*0.00000028

	return map[string]any{
		"question":             question,
		"rounds":               len(transcript) / 2,
		"transcript":           transcript,
		"decision":             decision,
		"adr":                  adr,
		"concession_detected":  concessionDetected,
		"cost":                 cost,
	}
}

func main() {
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
			resp.Result = map[string]string{"status": "ok", "agent": "adr-moderator"}

		case "debate":
			var params map[string]any
			json.Unmarshal(req.Params, &params)
			question, _ := params["question"].(string)
			if question == "" {
				question, _ = params["message"].(string)
			}
			if question == "" {
				resp.Error = map[string]any{"code": -32602, "message": "question is required"}
			} else {
				resp.Result = runDebate(question)
			}

		default:
			resp.Error = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
		}

		json.NewEncoder(os.Stdout).Encode(resp)
	}
}
