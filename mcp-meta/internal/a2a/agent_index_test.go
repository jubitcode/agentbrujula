package a2a

import (
	"testing"

	"mcp-meta/internal/registry"
)

// mockRegistry implements registry.Registry for testing.
type mockRegistry struct {
	agentPods []*registry.PodSpec
}

func (m *mockRegistry) LoadManifests(string) error                                    { return nil }
func (m *mockRegistry) AllTools() []registry.ToolInfo                                 { return nil }
func (m *mockRegistry) ResolveTool(string) (*registry.PodSpec, *registry.ToolDef, error) { return nil, nil, nil }
func (m *mockRegistry) GetPodSpec(string) *registry.PodSpec                           { return nil }
func (m *mockRegistry) PodsToPreWarm() []*registry.PodSpec                            { return nil }
func (m *mockRegistry) Validate() []error                                             { return nil }
func (m *mockRegistry) AllAgentPods() []*registry.PodSpec                              { return m.agentPods }
func (m *mockRegistry) RegisterManifest(manifest *registry.Manifest) error             { return nil }
func (m *mockRegistry) UnregisterPod(podName string) error                             { return nil }

func testPodSpec() *registry.PodSpec {
	return &registry.PodSpec{
		Name: "research-agent",
		AgentCard: &registry.AgentCardSpec{
			Name:        "Research Agent",
			Description: "Performs web research",
			Version:     "1.0.0",
			Provider: &registry.ProviderSpec{
				Organization: "TestOrg",
			},
			Capabilities: &registry.A2ACapabilities{
				Streaming: true,
			},
			Authentication: &registry.A2AAuthSpec{
				Schemes: []registry.AuthScheme{
					{Scheme: "bearer", BearerFormat: "JWT"},
				},
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		},
		Skills: []registry.SkillDef{
			{
				ID:          "web-research",
				Name:        "Web Research",
				Description: "Search the web",
				Tags:        []string{"research"},
				Examples:    []string{"Find papers on Go"},
				MapToTools:  []string{"web_search"},
			},
			{
				ID:          "pdf-extraction",
				Name:        "PDF Extraction",
				Description: "Extract PDF content",
				Tags:        []string{"pdf"},
				MapToTools:  []string{"pdf_read"},
			},
		},
	}
}

func TestNewAgentIndex_BuildsFromRegistry(t *testing.T) {
	reg := &mockRegistry{agentPods: []*registry.PodSpec{testPodSpec()}}
	idx := NewAgentIndex(reg)

	if !idx.HasAgents() {
		t.Fatal("expected HasAgents() to be true")
	}
}

func TestAgentIndex_ResolveSkill(t *testing.T) {
	reg := &mockRegistry{agentPods: []*registry.PodSpec{testPodSpec()}}
	idx := NewAgentIndex(reg)

	tests := []struct {
		name      string
		skillID   string
		wantFQN   string
		wantError bool
	}{
		{"valid skill", "web-research", "research-agent.web_search", false},
		{"valid skill 2", "pdf-extraction", "research-agent.pdf_read", false},
		{"unknown skill", "nonexistent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fqn, err := idx.ResolveSkill(tt.skillID)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fqn != tt.wantFQN {
				t.Errorf("got %q, want %q", fqn, tt.wantFQN)
			}
		})
	}
}

func TestAgentIndex_GetCard(t *testing.T) {
	reg := &mockRegistry{agentPods: []*registry.PodSpec{testPodSpec()}}
	idx := NewAgentIndex(reg)

	card := idx.GetCard("research-agent")
	if card == nil {
		t.Fatal("expected card for research-agent")
	}
	if card.Name != "Research Agent" {
		t.Errorf("card.Name = %q, want %q", card.Name, "Research Agent")
	}
	if card.Description != "Performs web research" {
		t.Errorf("card.Description = %q", card.Description)
	}
	if card.Version != "1.0.0" {
		t.Errorf("card.Version = %q", card.Version)
	}
	if card.Provider == nil || card.Provider.Organization != "TestOrg" {
		t.Error("expected provider TestOrg")
	}
	if card.Capabilities == nil || !card.Capabilities.Streaming {
		t.Error("expected streaming capability")
	}
	if card.Authentication == nil || len(card.Authentication.Schemes) != 1 {
		t.Error("expected 1 auth scheme")
	}
	if len(card.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(card.Skills))
	}
	if len(card.DefaultInputModes) != 1 || card.DefaultInputModes[0] != "text" {
		t.Errorf("unexpected DefaultInputModes: %v", card.DefaultInputModes)
	}

	// Non-existent pod
	if idx.GetCard("nonexistent") != nil {
		t.Error("expected nil for nonexistent pod")
	}
}

func TestAgentIndex_CompositeCard(t *testing.T) {
	pod2 := &registry.PodSpec{
		Name: "code-agent",
		AgentCard: &registry.AgentCardSpec{
			Name:        "Code Agent",
			Description: "Writes code",
			Version:     "2.0.0",
			Capabilities: &registry.A2ACapabilities{
				PushNotifications: true,
			},
		},
		Skills: []registry.SkillDef{
			{
				ID:          "code-gen",
				Name:        "Code Generation",
				Description: "Generate code",
				MapToTools:  []string{"generate"},
			},
		},
	}

	reg := &mockRegistry{agentPods: []*registry.PodSpec{testPodSpec(), pod2}}
	idx := NewAgentIndex(reg)

	card := idx.CompositeCard("http://localhost:3000")
	if card == nil {
		t.Fatal("expected composite card")
	}
	if card.URL != "http://localhost:3000" {
		t.Errorf("card.URL = %q", card.URL)
	}
	if card.Name != "MCP-Meta Agent" {
		t.Errorf("card.Name = %q", card.Name)
	}

	// Should have skills from both pods
	if len(card.Skills) != 3 {
		t.Errorf("expected 3 skills (2 + 1), got %d", len(card.Skills))
	}

	// Capabilities merged: streaming from pod1, push from pod2
	if card.Capabilities == nil {
		t.Fatal("expected capabilities")
	}
	if !card.Capabilities.Streaming {
		t.Error("expected streaming=true (from research-agent)")
	}
	if !card.Capabilities.PushNotifications {
		t.Error("expected pushNotifications=true (from code-agent)")
	}
}

func TestAgentIndex_CompositeCard_NoAgents(t *testing.T) {
	reg := &mockRegistry{agentPods: nil}
	idx := NewAgentIndex(reg)

	card := idx.CompositeCard("http://localhost:3000")
	if card != nil {
		t.Error("expected nil composite card when no agents")
	}
	if idx.HasAgents() {
		t.Error("expected HasAgents()=false")
	}
}

func TestAgentIndex_SkillWithMultipleTools(t *testing.T) {
	spec := &registry.PodSpec{
		Name: "multi-tool-agent",
		AgentCard: &registry.AgentCardSpec{
			Name:        "Multi-Tool Agent",
			Description: "Agent with multi-tool skill",
		},
		Skills: []registry.SkillDef{
			{
				ID:         "full-research",
				Name:       "Full Research",
				Description: "Multi-step research",
				MapToTools: []string{"web_search", "web_read", "summarize"},
			},
		},
	}

	reg := &mockRegistry{agentPods: []*registry.PodSpec{spec}}
	idx := NewAgentIndex(reg)

	// ResolveSkill returns first tool FQN
	fqn, err := idx.ResolveSkill("full-research")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fqn != "multi-tool-agent.web_search" {
		t.Errorf("expected first tool FQN, got %q", fqn)
	}
}
