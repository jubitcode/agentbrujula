package a2a

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"

	"mcp-meta/internal/registry"
)

// skillMapping maps a skill ID to its owning pod and tool FQNs.
type skillMapping struct {
	podName  string
	toolFQNs []string // "pod.tool" fully qualified names
}

// skillEmbedding caches the embedding vector for a skill's description.
type skillEmbedding struct {
	skillID   string
	text      string
	embedding []float64
}

// AgentIndex indexes A2A agent pods from the registry, mapping skill IDs
// to pod.tool FQNs and building Agent Cards from manifest configuration.
type AgentIndex struct {
	mu              sync.RWMutex
	skills          map[string]*skillMapping // skill ID → mapping
	cards           map[string]*AgentCard    // pod name → agent card
	embeddings      []skillEmbedding         // cached skill embeddings
	embeddingURL    string                   // LM Studio embeddings endpoint
	embeddingsReady bool
}

// NewAgentIndex scans the registry for AgentPod manifests and builds the index.
// If the embeddingURL is non-empty (e.g. "http://localhost:1234/v1"), skill descriptions
// are embedded at startup for semantic intent matching.
func NewAgentIndex(reg registry.Registry, embeddingURL ...string) *AgentIndex {
	idx := &AgentIndex{
		skills: make(map[string]*skillMapping),
		cards:  make(map[string]*AgentCard),
	}

	if len(embeddingURL) > 0 && embeddingURL[0] != "" {
		idx.embeddingURL = strings.TrimSuffix(embeddingURL[0], "/")
	}

	for _, podSpec := range reg.AllAgentPods() {
		idx.indexPod(podSpec)
	}

	// Pre-compute embeddings for all skills
	if idx.embeddingURL != "" {
		idx.precomputeEmbeddings()
	}

	return idx
}

func (idx *AgentIndex) precomputeEmbeddings() {
	slog.Info("embedding skills for intent classification", "url", idx.embeddingURL)
	for _, card := range idx.cards {
		for _, skill := range card.Skills {
			text := skill.Name + ": " + skill.Description
			if len(skill.Examples) > 0 {
				text += ". Examples: " + strings.Join(skill.Examples, ", ")
			}
			vec, err := idx.embed(text)
			if err != nil {
				slog.Warn("failed to embed skill, falling back to keyword matching", "skill", skill.ID, "error", err)
				return
			}
			slog.Info("embedded skill", "skill", skill.ID, "dimensions", len(vec))
			idx.embeddings = append(idx.embeddings, skillEmbedding{
				skillID:   skill.ID,
				text:      text,
				embedding: vec,
			})
		}
	}
	if len(idx.embeddings) > 0 {
		idx.embeddingsReady = true
		slog.Info("intent classification ready", "skills_embedded", len(idx.embeddings), "method", "cosine_similarity")
	}
}

func (idx *AgentIndex) embed(text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]any{
		"model": "text-embedding-qwen3-embedding-0.6b",
		"input": text,
	})
	resp, err := http.Post(idx.embeddingURL+"/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close embedding response body: %v", err)
		}
	}(resp.Body)
	data, _ := io.ReadAll(resp.Body)

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse embedding: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return result.Data[0].Embedding, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func (idx *AgentIndex) indexPod(spec *registry.PodSpec) {
	if spec.AgentCard == nil {
		return
	}

	card := buildCard(spec)
	idx.cards[spec.Name] = card

	for _, skill := range spec.Skills {
		fqns := make([]string, 0, len(skill.MapToTools))
		for _, toolName := range skill.MapToTools {
			fqns = append(fqns, fmt.Sprintf("%s.%s", spec.Name, toolName))
		}
		idx.skills[skill.ID] = &skillMapping{
			podName:  spec.Name,
			toolFQNs: fqns,
		}
	}
}

// ResolveSkill returns the first pod.tool FQN for a skill ID.
// For skills mapping to multiple tools, returns the first one; the pod
// itself orchestrates multi-tool workflows via __mcp_meta_call__.
func (idx *AgentIndex) ResolveSkill(skillID string) (string, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	mapping, ok := idx.skills[skillID]
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillID)
	}
	if len(mapping.toolFQNs) == 0 {
		return "", fmt.Errorf("skill %s has no mapped tools", skillID)
	}
	return mapping.toolFQNs[0], nil
}

// GetCard returns the Agent Card for a specific pod, or nil if not an agent pod.
func (idx *AgentIndex) GetCard(podName string) *AgentCard {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.cards[podName]
}

// CompositeCard merges all agent pod skills into a single Agent Card.
// This is served at /.well-known/agent-card.json.
func (idx *AgentIndex) CompositeCard(baseURL string) *AgentCard {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.cards) == 0 {
		return nil
	}

	composite := &AgentCard{
		Name:               "MCP-Meta Agent",
		Description:        "Polyglot agent orchestrator",
		URL:                baseURL,
		Version:            "1.0.0",
		ProtocolVersion:    "1.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	var allSkills []Skill
	streaming := false
	pushNotifications := false

	for _, card := range idx.cards {
		allSkills = append(allSkills, card.Skills...)

		if card.Capabilities != nil {
			if card.Capabilities.Streaming {
				streaming = true
			}
			if card.Capabilities.PushNotifications {
				pushNotifications = true
			}
		}

		// Use first card's provider/auth if composite doesn't have one
		if composite.Provider == nil && card.Provider != nil {
			composite.Provider = card.Provider
		}
		if composite.Authentication == nil && card.Authentication != nil {
			composite.Authentication = card.Authentication
		}
	}

	composite.Skills = allSkills
	composite.Capabilities = &Capabilities{
		Streaming:         streaming,
		PushNotifications: pushNotifications,
	}

	return composite
}

// ClassifyIntent maps natural language text to the best matching skill ID.
// Uses cosine similarity on embeddings when available, falls back to keyword matching.
func (idx *AgentIndex) ClassifyIntent(text string) string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.skills) == 0 {
		return ""
	}

	// Try embedding-based matching first
	if idx.embeddingsReady {
		if skillID := idx.classifyByEmbedding(text); skillID != "" {
			return skillID
		}
	}

	// Fallback: keyword pattern matching
	return idx.classifyByKeywords(text)
}

func (idx *AgentIndex) classifyByEmbedding(text string) string {
	queryVec, err := idx.embed(text)
	if err != nil {
		slog.Warn("failed to embed query, falling back to keywords", "error", err)
		return ""
	}

	bestID := ""
	bestSim := 0.0

	for _, se := range idx.embeddings {
		sim := cosineSimilarity(queryVec, se.embedding)
		if sim > bestSim {
			bestSim = sim
			bestID = se.skillID
		}
	}

	slog.Info("intent classified",
		"query", text[:min(len(text), 60)],
		"matched_skill", bestID,
		"similarity", fmt.Sprintf("%.3f", bestSim),
	)

	if bestSim < 0.3 {
		// Low confidence — fall back to research
		if _, exists := idx.skills["deep-research"]; exists {
			return "deep-research"
		}
	}

	return bestID
}

func (idx *AgentIndex) classifyByKeywords(text string) string {
	lower := strings.ToLower(text)

	// Pattern-based routing
	patterns := map[string][]string{
		"content-write":     {"write", "blog", "article", "post", "draft", "essay"},
		"competitive-intel": {"intel", "competitor", "competitive", "versus", " vs "},
		"trend-watch":       {"trend", "trending", "monitor"},
	}

	for skillID, keywords := range patterns {
		if _, exists := idx.skills[skillID]; !exists {
			continue
		}
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				return skillID
			}
		}
	}

	// Default to research
	if _, exists := idx.skills["deep-research"]; exists {
		return "deep-research"
	}
	for id := range idx.skills {
		return id
	}
	return ""
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	// Split on non-alphanumeric
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	// Filter short words
	result := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > 2 {
			result = append(result, w)
		}
	}
	return result
}

// HasAgents returns true if any agent pods are indexed.
func (idx *AgentIndex) HasAgents() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.cards) > 0
}

// buildCard converts a PodSpec's AgentCard config into an A2A AgentCard JSON type.
func buildCard(spec *registry.PodSpec) *AgentCard {
	ac := spec.AgentCard
	card := &AgentCard{
		Name:        ac.Name,
		Description: ac.Description,
		URL:         ac.URL,
		Version:     ac.Version,
	}

	if ac.Provider != nil {
		card.Provider = &Provider{
			Organization: ac.Provider.Organization,
			URL:          ac.Provider.URL,
		}
	}

	if ac.Capabilities != nil {
		card.Capabilities = &Capabilities{
			Streaming:         ac.Capabilities.Streaming,
			PushNotifications: ac.Capabilities.PushNotifications,
		}
	}

	if ac.Authentication != nil {
		schemes := make([]AuthSchemeInfo, 0, len(ac.Authentication.Schemes))
		for _, s := range ac.Authentication.Schemes {
			schemes = append(schemes, AuthSchemeInfo{
				Scheme:       s.Scheme,
				BearerFormat: s.BearerFormat,
			})
		}
		card.Authentication = &Authentication{Schemes: schemes}
	}

	card.DefaultInputModes = ac.DefaultInputModes
	if card.DefaultInputModes == nil {
		card.DefaultInputModes = []string{"text"}
	}
	card.DefaultOutputModes = ac.DefaultOutputModes
	if card.DefaultOutputModes == nil {
		card.DefaultOutputModes = []string{"text"}
	}

	skills := make([]Skill, 0, len(spec.Skills))
	for _, sd := range spec.Skills {
		skills = append(skills, Skill{
			ID:          sd.ID,
			Name:        sd.Name,
			Description: sd.Description,
			Tags:        sd.Tags,
			Examples:    sd.Examples,
		})
	}
	card.Skills = skills

	return card
}
