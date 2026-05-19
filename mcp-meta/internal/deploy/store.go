package deploy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Status DeployStatus represents the outcome of a deployment.
type Status string

const (
	DeployStatusSuccess Status = "success"
	DeployStatusFailed  Status = "failed"
)

// StepResult records the outcome of one deployment step.
type StepResult struct {
	Step       string `json:"step"`
	Status     string `json:"status"` // "pass" or "fail"
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
	Details    any    `json:"details,omitempty"`
}

// Record DeployRecord is a single deployment entry.
type Record struct {
	ID           string       `json:"id"`
	Agent        string       `json:"agent"`
	Version      int          `json:"version"`
	Status       Status       `json:"status"`
	Steps        []StepResult `json:"steps"`
	FailedStep   string       `json:"failed_step,omitempty"`
	Error        string       `json:"error,omitempty"`
	ManifestYAML string       `json:"manifest_yaml,omitempty"`
	ImageTag     string       `json:"image_tag,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	DurationMs   int64        `json:"total_duration_ms"`
}

// Store DeployStore persists deployment records in SQLite.
type Store struct {
	db      *sql.DB
	dataDir string // ~/.mcp-meta/deployments/
}

// NewDeployStore opens or creates the SQLite database.
func NewDeployStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "registry.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db, dataDir: dataDir}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS deployments (
			id TEXT PRIMARY KEY,
			agent TEXT NOT NULL,
			version INTEGER NOT NULL,
			status TEXT NOT NULL,
			steps_json TEXT,
			failed_step TEXT,
			error TEXT,
			manifest_yaml TEXT,
			image_tag TEXT,
			created_at TEXT NOT NULL,
			duration_ms INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_deployments_agent ON deployments(agent);
		CREATE INDEX IF NOT EXISTS idx_deployments_agent_version ON deployments(agent, version);
	`)
	return err
}

// Create inserts a new deployment record.
func (s *Store) Create(record *Record) error {
	stepsJSON, _ := json.Marshal(record.Steps)
	_, err := s.db.Exec(`
		INSERT INTO deployments (id, agent, version, status, steps_json, failed_step, error, manifest_yaml, image_tag, created_at, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.Agent, record.Version, record.Status,
		string(stepsJSON), record.FailedStep, record.Error,
		record.ManifestYAML, record.ImageTag,
		record.CreatedAt.UTC().Format(time.RFC3339), record.DurationMs,
	)
	return err
}

// Get retrieves a deployment by ID.
func (s *Store) Get(id string) (*Record, error) {
	return s.scanOne(s.db.QueryRow(`SELECT * FROM deployments WHERE id = ?`, id))
}

// LatestByAgent returns the most recent deployment for an agent.
func (s *Store) LatestByAgent(agent string) (*Record, error) {
	return s.scanOne(s.db.QueryRow(
		`SELECT * FROM deployments WHERE agent = ? ORDER BY version DESC LIMIT 1`, agent))
}

// ListByAgent returns all deployments for an agent, newest first.
func (s *Store) ListByAgent(agent string) ([]*Record, error) {
	rows, err := s.db.Query(
		`SELECT * FROM deployments WHERE agent = ? ORDER BY version DESC`, agent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAll(rows)
}

// ListAll returns the latest deployment for each agent.
func (s *Store) ListAll() ([]*Record, error) {
	rows, err := s.db.Query(`
		SELECT d.* FROM deployments d
		INNER JOIN (
			SELECT agent, MAX(version) as max_version
			FROM deployments GROUP BY agent
		) latest ON d.agent = latest.agent AND d.version = latest.max_version
		ORDER BY d.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAll(rows)
}

// NextVersion returns the next version number for an agent.
func (s *Store) NextVersion(agent string) (int, error) {
	var maxVersion sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(version) FROM deployments WHERE agent = ?`, agent).Scan(&maxVersion)
	if err != nil {
		return 1, nil
	}
	if !maxVersion.Valid {
		return 1, nil
	}
	return int(maxVersion.Int64) + 1, nil
}

// Delete removes all deployments for an agent.
func (s *Store) Delete(agent string) error {
	_, err := s.db.Exec(`DELETE FROM deployments WHERE agent = ?`, agent)
	return err
}

// AgentDir returns the filesystem path for an agent's deployment files.
func (s *Store) AgentDir(agent string, version int) string {
	return filepath.Join(s.dataDir, agent, fmt.Sprintf("v%d", version))
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) scanOne(row *sql.Row) (*Record, error) {
	r := &Record{}
	var stepsJSON, createdAt string
	err := row.Scan(
		&r.ID, &r.Agent, &r.Version, &r.Status,
		&stepsJSON, &r.FailedStep, &r.Error,
		&r.ManifestYAML, &r.ImageTag,
		&createdAt, &r.DurationMs,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(stepsJSON), &r.Steps)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return r, nil
}

func (s *Store) scanAll(rows *sql.Rows) ([]*Record, error) {
	var records []*Record
	for rows.Next() {
		r := &Record{}
		var stepsJSON, createdAt string
		err := rows.Scan(
			&r.ID, &r.Agent, &r.Version, &r.Status,
			&stepsJSON, &r.FailedStep, &r.Error,
			&r.ManifestYAML, &r.ImageTag,
			&createdAt, &r.DurationMs,
		)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(stepsJSON), &r.Steps)
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		records = append(records, r)
	}
	return records, rows.Err()
}
