package a2a

// AgentCard is the JSON structure served at /.well-known/agent-card.json per the A2A spec.
type AgentCard struct {
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	URL                string            `json:"url"`
	Version            string            `json:"version"`
	ProtocolVersion    string            `json:"protocolVersion,omitempty"`
	Provider           *Provider         `json:"provider,omitempty"`
	DocumentationURL   string            `json:"documentationUrl,omitempty"`
	Capabilities       *Capabilities     `json:"capabilities,omitempty"`
	Authentication     *Authentication   `json:"authentication,omitempty"`
	DefaultInputModes  []string          `json:"defaultInputModes"`
	DefaultOutputModes []string          `json:"defaultOutputModes"`
	Skills             []Skill           `json:"skills"`
}

// Provider identifies who provides the agent.
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// Capabilities declares which A2A features the agent supports.
type Capabilities struct {
	Streaming              bool `json:"streaming,omitempty"`
	PushNotifications      bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
}

// Authentication declares supported auth schemes.
type Authentication struct {
	Schemes []AuthSchemeInfo `json:"schemes"`
}

// AuthSchemeInfo describes a single authentication method.
type AuthSchemeInfo struct {
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
}

// Skill describes an agent capability in the A2A Agent Card.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// TaskState represents an A2A task lifecycle state.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
)

// IsTerminal returns true if the state is a final state.
func (s TaskState) IsTerminal() bool {
	return s == TaskStateCompleted || s == TaskStateFailed || s == TaskStateCanceled
}

// Task represents an A2A task.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	Status    TaskStatus `json:"status"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	History   []Message  `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskStatus holds the current state and timestamp of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp"`
}

// Message represents an A2A message (user or agent role).
type Message struct {
	Role      string `json:"role"` // "user" or "agent"
	Parts     []Part `json:"parts"`
	MessageID string `json:"messageId,omitempty"`
}

// Part is a content unit within a message or artifact.
type Part struct {
	Kind     string `json:"kind"` // "text", "data", "file"
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     any    `json:"data,omitempty"`
}

// Artifact represents output produced by a task.
type Artifact struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}
