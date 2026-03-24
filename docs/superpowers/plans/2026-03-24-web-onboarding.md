# Web-Based Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow vedetta to start without a config file and guide users through setup via the web UI.

**Architecture:** The binary detects a missing config and enters "setup mode," serving only onboarding endpoints. After the user creates an admin account and optionally discovers cameras, a config file is written and the system transitions to full operation without restart. Auth moves to DB-primary with config as seed.

**Tech Stack:** Go, gopkg.in/yaml.v3 (yaml.Node for structure-preserving writes), SQLite, htmx + vanilla JS, bcrypt

**Spec:** `docs/superpowers/specs/2026-03-24-web-onboarding-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/config.go` | Modify | Add `LoadOrDefault()`, relax camera validation |
| `internal/config/config_test.go` | Modify | Tests for new loading behavior |
| `internal/config/write.go` | Create | YAML config file writing with `yaml.Node` |
| `internal/config/write_test.go` | Create | Tests for config writing |
| `internal/storage/db.go` | Modify | Add `auth_users` table, seed/query methods |
| `internal/storage/db_test.go` | Modify | Tests for auth user DB operations |
| `internal/auth/auth.go` | Modify | Switch to DB-primary auth with config seed |
| `internal/auth/auth_test.go` | Modify | Tests for DB-based auth |
| `internal/api/setup.go` | Create | Setup mode handlers (POST /api/setup, discover, cameras) |
| `internal/api/setup_test.go` | Create | Tests for setup API |
| `internal/api/server.go` | Modify | Setup mode routing, ready gate changes |
| `internal/api/static/setup.html` | Create | Onboarding UI (account + discovery screens) |
| `internal/camera/manager.go` | Modify | Add `AddCamera()` for hot-adding |
| `cmd/vedetta/main.go` | Modify | Setup mode startup path, auth seed flow |

---

## Task 1: Config Loading — `LoadOrDefault` and Relaxed Validation

**Files:**
- Modify: `internal/config/config.go:149-285`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write test for LoadOrDefault with missing file**

In `internal/config/config_test.go`, add:

```go
func TestLoadOrDefault_MissingFile(t *testing.T) {
	cfg, setupMode, err := LoadOrDefault("/tmp/vedetta-test-nonexistent.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !setupMode {
		t.Fatal("expected setupMode=true for missing file")
	}
	if cfg.API.Port != 5050 {
		t.Errorf("expected default port 5050, got %d", cfg.API.Port)
	}
	if cfg.Detect.ScoreThreshold != 0.65 {
		t.Errorf("expected default score threshold 0.65, got %f", cfg.Detect.ScoreThreshold)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadOrDefault_MissingFile -v`
Expected: FAIL — `LoadOrDefault` not defined

- [ ] **Step 3: Write test for LoadOrDefault with valid file**

```go
func TestLoadOrDefault_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`
auth:
  users:
    - username: admin
      password_hash: "$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu"
cameras:
  - name: test
    url: rtsp://localhost/stream
`), 0644)

	cfg, setupMode, err := LoadOrDefault(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setupMode {
		t.Fatal("expected setupMode=false for valid file")
	}
	if len(cfg.Cameras) != 1 {
		t.Errorf("expected 1 camera, got %d", len(cfg.Cameras))
	}
}
```

- [ ] **Step 4: Write test for LoadOrDefault with invalid file**

```go
func TestLoadOrDefault_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`invalid: [yaml`), 0644)

	_, _, err := LoadOrDefault(path)
	if err == nil {
		t.Fatal("expected error for invalid file")
	}
}
```

- [ ] **Step 5: Write test for zero cameras being valid**

```go
func TestLoad_ZeroCamerasValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	os.WriteFile(path, []byte(`
auth:
  users:
    - username: admin
      password_hash: "$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu"
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("zero cameras should be valid, got error: %v", err)
	}
	if len(cfg.Cameras) != 0 {
		t.Errorf("expected 0 cameras, got %d", len(cfg.Cameras))
	}
}
```

- [ ] **Step 6: Implement LoadOrDefault and relax camera validation**

In `internal/config/config.go`:

```go
// LoadOrDefault loads config from path, or returns defaults if file is missing.
// Returns (config, setupMode, error). setupMode is true when the file does not exist.
func LoadOrDefault(path string) (*Config, bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Defaults()
		return cfg, true, nil
	}
	cfg, err := Load(path)
	if err != nil {
		return nil, false, err
	}
	return cfg, false, nil
}

// Defaults returns a Config with all default values applied.
// Exported for use in main.go (read-only config fallback).
func Defaults() *Config {
	return &Config{
		Detect: DetectConfig{
			ScoreThreshold:       0.65,
			ObjectMatchThreshold: 0.65,
			Motion: MotionConfig{
				PixelThreshold:  25,
				MinArea:         200,
				BackgroundAlpha: 0.05,
				MinRegionScore:  0.02,
			},
			Labels: []string{"person", "car", "truck", "bus", "motorcycle", "bicycle", "dog", "cat", "bird"},
		},
		Recording: RecordingConfig{
			Path:             "./recordings",
			PreCapture:       5 * time.Second,
			PostCapture:      10 * time.Second,
			MaxEventDuration: 2 * time.Minute,
			RetainDays:       7,
			EventRetain:      30,
			SegmentLength:    10 * time.Minute,
			Continuous:       true,
		},
		Events: EventConfig{
			CooldownSeconds: 30,
			RetainDays:      90,
			SnapshotPath:    "./snapshots",
			SnapshotQuality: 85,
		},
		Storage: StorageConfig{
			DBPath: "./vedetta.db",
		},
		API: APIConfig{
			Host:     "0.0.0.0",
			Port:     5050,
			Exposure: "lan",
		},
		RTSPServer: RTSPServerConfig{
			Port: 8554,
		},
	}
}
```

Also refactor `Load()` to use `defaults()` instead of duplicating the default values (lines 155-194), and remove the camera count check at line 224-226:

```go
// In Load(), replace the inline defaults with:
cfg := Defaults()

// Remove this block (line 224-226):
// if len(cfg.Cameras) == 0 {
//     return nil, fmt.Errorf("at least one camera must be configured")
// }
```

- [ ] **Step 7: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: All tests pass, including existing tests

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add LoadOrDefault and allow zero cameras"
```

---

## Task 2: Config File Writing with yaml.Node

**Files:**
- Create: `internal/config/write.go`
- Create: `internal/config/write_test.go`

- [ ] **Step 1: Write test for generating initial config**

In `internal/config/write_test.go`:

```go
func TestWriteInitialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	err := WriteInitialConfig(path, "admin", "$2a$10$hashhere")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("generated config should be loadable: %v", err)
	}
	if len(cfg.Auth.Users) != 1 {
		t.Fatalf("expected 1 auth user, got %d", len(cfg.Auth.Users))
	}
	if cfg.Auth.Users[0].Username != "admin" {
		t.Errorf("expected username 'admin', got %q", cfg.Auth.Users[0].Username)
	}
	if cfg.API.Port != 5050 {
		t.Errorf("expected port 5050, got %d", cfg.API.Port)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestWriteInitialConfig -v`
Expected: FAIL — `WriteInitialConfig` not defined

- [ ] **Step 3: Write test for appending cameras**

```go
func TestAppendCamera(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	// Write initial config
	err := WriteInitialConfig(path, "admin", "$2a$10$hashhere")
	if err != nil {
		t.Fatalf("WriteInitialConfig: %v", err)
	}

	cam := CameraConfig{
		Name:      "front_door",
		URL:       "rtsp://admin:pass@192.168.1.100:554/stream2",
		RecordURL: "rtsp://admin:pass@192.168.1.100:554/stream1",
		Detect:    DetectStreamConfig{Width: 640, Height: 480, FPS: 5},
		Record:    StreamConfig{Width: 1920, Height: 1080, FPS: 15},
	}
	err = AppendCamera(path, cam, "TP-Link Tapo C200 (192.168.1.100)")
	if err != nil {
		t.Fatalf("AppendCamera: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("config should still be loadable: %v", err)
	}
	if len(cfg.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].Name != "front_door" {
		t.Errorf("expected camera name 'front_door', got %q", cfg.Cameras[0].Name)
	}
	if cfg.Auth.Users[0].Username != "admin" {
		t.Error("auth section should be preserved after camera append")
	}
}
```

- [ ] **Step 4: Write test for appending multiple cameras preserves existing ones**

```go
func TestAppendCamera_Multiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	WriteInitialConfig(path, "admin", "$2a$10$hashhere")
	AppendCamera(path, CameraConfig{Name: "cam1", URL: "rtsp://1"}, "")
	AppendCamera(path, CameraConfig{Name: "cam2", URL: "rtsp://2"}, "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].Name != "cam1" || cfg.Cameras[1].Name != "cam2" {
		t.Error("cameras should be in insertion order")
	}
}
```

- [ ] **Step 5: Write test for read-only path returns YAML string**

```go
func TestWriteInitialConfig_ReadOnly(t *testing.T) {
	// Use a path in a non-existent directory to simulate read-only
	path := "/proc/vedetta-readonly/config.yml"

	yaml, err := GenerateInitialConfigYAML("admin", "$2a$10$hashhere")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(yaml, "username: admin") {
		t.Error("generated YAML should contain username")
	}
	if !strings.Contains(yaml, "password_hash:") {
		t.Error("generated YAML should contain password_hash")
	}
	_ = path // read-only detection is at the API layer
}
```

- [ ] **Step 6: Implement config writing**

Create `internal/config/write.go`:

```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// WriteInitialConfig writes a new config file with auth credentials and defaults.
func WriteInitialConfig(path, username, passwordHash string) error {
	content, err := GenerateInitialConfigYAML(username, passwordHash)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// GenerateInitialConfigYAML returns the YAML string for an initial config.
// Used both for writing to disk and for the read-only fallback (show YAML to user).
func GenerateInitialConfigYAML(username, passwordHash string) (string, error) {
	type initialConfig struct {
		Auth      AuthConfig      `yaml:"auth"`
		API       APIConfig       `yaml:"api"`
		Storage   StorageConfig   `yaml:"storage"`
		Recording writableRecCfg  `yaml:"recording"`
		Events    EventConfig     `yaml:"events"`
		Detect    writableDetCfg  `yaml:"detect"`
	}

	cfg := initialConfig{
		Auth: AuthConfig{
			Users: []AuthUser{{
				Username:     username,
				PasswordHash: passwordHash,
			}},
		},
		API: APIConfig{
			Host:     "0.0.0.0",
			Port:     5050,
			Exposure: "lan",
		},
		Storage: StorageConfig{
			DBPath: "./vedetta.db",
		},
		Recording: writableRecCfg{
			Path:          "./recordings",
			Continuous:    true,
			SegmentLength: "10m",
			PreCapture:    "5s",
			PostCapture:   "10s",
			RetainDays:    7,
			EventRetain:   30,
		},
		Events: EventConfig{
			CooldownSeconds: 30,
			RetainDays:      90,
			SnapshotPath:    "./snapshots",
			SnapshotQuality: 85,
		},
		Detect: writableDetCfg{
			ScoreThreshold: 0.65,
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(data), nil
}

// writableRecCfg uses string durations for YAML output (time.Duration marshals as nanoseconds).
type writableRecCfg struct {
	Path          string `yaml:"path"`
	Continuous    bool   `yaml:"continuous"`
	SegmentLength string `yaml:"segment_length"`
	PreCapture    string `yaml:"pre_capture"`
	PostCapture   string `yaml:"post_capture"`
	RetainDays    int    `yaml:"retain_days"`
	EventRetain   int    `yaml:"event_retain_days"`
}

// writableDetCfg is a minimal detect section for initial config.
type writableDetCfg struct {
	ScoreThreshold float32 `yaml:"score_threshold"`
}

// AppendCamera adds a camera to an existing config file using yaml.Node
// to preserve structure and comments.
func AppendCamera(path string, cam CameraConfig, comment string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at root")
	}

	// Find or create the "cameras" key
	var camerasSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "cameras" {
			camerasSeq = root.Content[i+1]
			break
		}
	}
	if camerasSeq == nil {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: "cameras", Tag: "!!str"}
		seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, keyNode, seqNode)
		camerasSeq = seqNode
	}

	// Build camera node
	camNode := buildCameraNode(cam, comment)
	camerasSeq.Content = append(camerasSeq.Content, camNode)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, out, 0600)
}

// GenerateCameraYAML returns a YAML snippet for a camera (read-only fallback).
func GenerateCameraYAML(cam CameraConfig, comment string) (string, error) {
	camNode := buildCameraNode(cam, comment)
	// Wrap in a sequence for proper YAML output
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{camNode}}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "cameras", Tag: "!!str"},
			seq,
		}},
	}}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildCameraNode(cam CameraConfig, comment string) *yaml.Node {
	fields := []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: cam.Name, Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: "url", Tag: "!!str"},
		{Kind: yaml.ScalarNode, Value: cam.URL, Tag: "!!str"},
	}

	if cam.RecordURL != "" {
		fields = append(fields,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "record_url", Tag: "!!str"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: cam.RecordURL, Tag: "!!str"},
		)
	}

	// detect block
	fields = append(fields,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "detect", Tag: "!!str"},
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "width", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Detect.Width), Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: "height", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Detect.Height), Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: "fps", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Detect.FPS), Tag: "!!int"},
		}},
	)

	// record block
	fields = append(fields,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "record", Tag: "!!str"},
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "width", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Record.Width), Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: "height", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Record.Height), Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: "fps", Tag: "!!int"},
			{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", cam.Record.FPS), Tag: "!!int"},
		}},
	)

	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: fields}
	if comment != "" {
		node.HeadComment = comment
	}
	return node
}
```

- [ ] **Step 7: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/config/write.go internal/config/write_test.go
git commit -m "feat(config): add config file writing with yaml.Node preservation"
```

---

## Task 3: Auth Users in Database

**Files:**
- Modify: `internal/storage/db.go:58-225` (migrate function, add table + methods)
- Modify: `internal/storage/db_test.go`

- [ ] **Step 1: Write tests for auth user DB operations**

In `internal/storage/db_test.go`, add:

```go
func TestAuthUsers(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Save user
	err := db.SaveAuthUser("admin", "$2a$10$hash1")
	if err != nil {
		t.Fatalf("SaveAuthUser: %v", err)
	}

	// List users
	users, err := db.ListAuthUsers()
	if err != nil {
		t.Fatalf("ListAuthUsers: %v", err)
	}
	if len(users) != 1 || users[0].Username != "admin" {
		t.Errorf("expected 1 user 'admin', got %v", users)
	}

	// Save again — should not duplicate
	err = db.SaveAuthUser("admin", "$2a$10$hash2")
	if err != nil {
		t.Fatalf("SaveAuthUser duplicate: %v", err)
	}
	users, _ = db.ListAuthUsers()
	if len(users) != 1 {
		t.Errorf("expected still 1 user after re-save, got %d", len(users))
	}

	// Seed — inserts only if not present
	err = db.SeedAuthUser("admin", "$2a$10$oldhash")
	if err != nil {
		t.Fatalf("SeedAuthUser: %v", err)
	}
	users, _ = db.ListAuthUsers()
	if users[0].PasswordHash != "$2a$10$hash2" {
		t.Error("SeedAuthUser should not overwrite existing user")
	}

	// Seed new user
	err = db.SeedAuthUser("viewer", "$2a$10$viewerhash")
	if err != nil {
		t.Fatalf("SeedAuthUser new: %v", err)
	}
	users, _ = db.ListAuthUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users after seed, got %d", len(users))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestAuthUsers -v`
Expected: FAIL — `SaveAuthUser` not defined

- [ ] **Step 3: Implement auth_users table and methods**

In `internal/storage/db.go`, add to the `migrate()` function (after existing table creation):

```go
_, err = db.Exec(`CREATE TABLE IF NOT EXISTS auth_users (
	username TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`)
if err != nil {
	return fmt.Errorf("create auth_users: %w", err)
}
```

Add methods:

```go
// SaveAuthUser creates or updates an auth user.
func (db *DB) SaveAuthUser(username, passwordHash string) error {
	_, err := db.db.Exec(
		`INSERT INTO auth_users (username, password_hash, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash, updated_at=CURRENT_TIMESTAMP`,
		username, passwordHash,
	)
	return err
}

// SeedAuthUser inserts a user only if it does not already exist.
func (db *DB) SeedAuthUser(username, passwordHash string) error {
	_, err := db.db.Exec(
		`INSERT OR IGNORE INTO auth_users (username, password_hash) VALUES (?, ?)`,
		username, passwordHash,
	)
	return err
}

// ListAuthUsers returns all auth users.
func (db *DB) ListAuthUsers() ([]AuthUser, error) {
	rows, err := db.db.Query(`SELECT username, password_hash FROM auth_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []AuthUser
	for rows.Next() {
		var u AuthUser
		if err := rows.Scan(&u.Username, &u.PasswordHash); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// AuthUser represents a stored auth user.
type AuthUser struct {
	Username     string
	PasswordHash string
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/storage/ -run TestAuthUsers -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/db.go internal/storage/db_test.go
git commit -m "feat(storage): add auth_users table with save, seed, and list"
```

---

## Task 4: Switch Auth to DB-Primary with Config Seed

**Files:**
- Modify: `internal/auth/auth.go:60-101` (Checker struct and New)
- Modify: `internal/auth/auth_test.go`
- Modify: `cmd/vedetta/main.go:60-63` (replace ValidateConfig)

- [ ] **Step 1: Write test for DB-based auth login**

In `internal/auth/auth_test.go`:

```go
func TestChecker_DBAuth(t *testing.T) {
	// Create a test DB — note: newTestDB is unexported in storage package,
	// so tests in auth package must use storage.New() with a temp dir.
	dir := t.TempDir()
	db, err := storage.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer db.Close()

	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	db.SaveAuthUser("admin", string(hash))

	checker := NewFromDB(config.APIConfig{Exposure: "lan"}, db)
	defer checker.Close()

	// Valid login
	session, err := checker.Login("admin", "secret", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("Login should succeed: %v", err)
	}
	if session.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", session.Username)
	}

	// Invalid password
	_, err = checker.Login("admin", "wrong", "127.0.0.1", "test")
	if err == nil {
		t.Error("Login with wrong password should fail")
	}

	// Unknown user
	_, err = checker.Login("nobody", "secret", "127.0.0.1", "test")
	if err == nil {
		t.Error("Login with unknown user should fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestChecker_DBAuth -v`
Expected: FAIL — `NewFromDB` not defined

- [ ] **Step 3: Implement NewFromDB constructor**

In `internal/auth/auth.go`, add a new constructor that reads users from DB instead of config:

```go
// NewFromDB creates a Checker that reads auth users from the database.
// Note: The existing Checker.users field is map[string][]byte (bcrypt hashes as byte slices).
// Match this type exactly.
func NewFromDB(apiCfg config.APIConfig, db *storage.DB) *Checker {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-timing-safe"), bcrypt.MinCost)
	c := &Checker{
		users:          make(map[string][]byte),
		dummyHash:      dummyHash,
		db:             db,
		exposure:       apiCfg.Exposure,
		trustedProxies: parseTrustedProxies(apiCfg.TrustedProxies),
		loginFailures:  make(map[string]*failureRecord),
		tokenCreates:   make(map[string]*failureRecord),
		done:           make(chan struct{}),
	}
	c.reloadUsers()
	go c.cleanupLoop()
	return c
}

// reloadUsers fetches auth users from DB and updates the in-memory map.
func (c *Checker) reloadUsers() {
	users, err := c.db.ListAuthUsers()
	if err != nil {
		slog.Error("failed to load auth users from DB", "error", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.users = make(map[string][]byte, len(users))
	for _, u := range users {
		c.users[u.Username] = []byte(u.PasswordHash)
	}
}
```

The new constructor mirrors the existing `New()` initialization pattern (see `auth.go:73-101`) but reads users from DB instead of config. The `Checker.users` field is `map[string][]byte` — password hashes must be stored as `[]byte` for `bcrypt.CompareHashAndPassword`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/auth/ -v`
Expected: All tests pass (both old config-based and new DB-based)

- [ ] **Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): add DB-primary auth with NewFromDB constructor"
```

---

## Task 5: Auth User Seeding in Main

**Files:**
- Modify: `cmd/vedetta/main.go:54-63`

- [ ] **Step 1: Implement config → DB seed flow**

In `cmd/vedetta/main.go`, after DB initialization and before auth checker creation, add the seed flow:

```go
// Seed auth users from config into DB (insert if not present, don't overwrite)
for _, user := range cfg.Auth.Users {
	if err := db.SeedAuthUser(user.Username, user.PasswordHash); err != nil {
		slog.Error("failed to seed auth user", "username", user.Username, "error", err)
	}
}

// Use DB-primary auth
authChecker := auth.NewFromDB(cfg.API, db)
```

Remove the old `auth.ValidateConfig(cfg.Auth)` call and `auth.New(cfg.Auth, cfg.API, db)` call.

- [ ] **Step 2: Verify the build compiles**

Run: `go build ./cmd/vedetta/`
Expected: Compiles without errors

- [ ] **Step 3: Run existing tests**

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add cmd/vedetta/main.go
git commit -m "feat(auth): seed config users into DB on startup"
```

---

## Task 6: Setup Mode API Handlers

**Files:**
- Create: `internal/api/setup.go`
- Create: `internal/api/setup_test.go`

- [ ] **Step 1: Write test for POST /api/setup**

In `internal/api/setup_test.go`:

```go
// Helper for setup tests — creates a test DB in a temp dir.
// storage.newTestDB is unexported, so cross-package tests use storage.New() directly.
func setupTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSetupHandler_CreateAccount(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	db := setupTestDB(t)

	setupDone := make(chan struct{}, 1)
	s := NewSetupHandler(configPath, db, setupDone)

	body := `{"username":"admin","password":"secret123"}`
	req := httptest.NewRequest("POST", "/api/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleSetup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Config file should exist with auth
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config should be loadable: %v", err)
	}
	if len(cfg.Auth.Users) != 1 {
		t.Errorf("expected 1 auth user in config")
	}

	// User should be in DB
	users, _ := db.ListAuthUsers()
	if len(users) != 1 || users[0].Username != "admin" {
		t.Errorf("expected admin user in DB")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestSetupHandler_CreateAccount -v`
Expected: FAIL — `NewSetupHandler` not defined

- [ ] **Step 3: Write test for POST /api/setup when config path is read-only**

```go
func TestSetupHandler_ReadOnly(t *testing.T) {
	configPath := "/proc/vedetta-readonly/config.yml"
	db := setupTestDB(t)

	setupDone := make(chan struct{}, 1)
	s := NewSetupHandler(configPath, db, setupDone)

	body := `{"username":"admin","password":"secret123"}`
	req := httptest.NewRequest("POST", "/api/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleSetup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even for read-only, got %d", w.Code)
	}

	// Response should contain YAML for manual save
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["config_yaml"]; !ok {
		t.Error("expected config_yaml in response for read-only path")
	}

	// User should still be in DB
	users, _ := db.ListAuthUsers()
	if len(users) != 1 {
		t.Error("user should be saved to DB even if config write fails")
	}
}
```

- [ ] **Step 4: Write test for GET /api/discover**

```go
func TestSetupHandler_Discover(t *testing.T) {
	db := setupTestDB(t)
	s := NewSetupHandler("", db, nil)

	req := httptest.NewRequest("GET", "/api/discover", nil)
	w := httptest.NewRecorder()

	s.HandleDiscover(w, req)

	// Should return 200 with cameras array (possibly empty on test network)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["cameras"]; !ok {
		t.Error("expected cameras array in response")
	}
}
```

- [ ] **Step 5: Write test for POST /api/cameras**

```go
func TestSetupHandler_AddCameras(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	db := setupTestDB(t)

	setupDone := make(chan struct{}, 1)
	s := NewSetupHandler(configPath, db, setupDone)

	// First create the initial config
	config.WriteInitialConfig(configPath, "admin", "$2a$10$fakehash")

	body := `{"cameras":[{"name":"front","url":"rtsp://a:b@1.2.3.4/s2","record_url":"rtsp://a:b@1.2.3.4/s1"}]}`
	req := httptest.NewRequest("POST", "/api/cameras", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleAddCameras(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Config should have the camera
	cfg, _ := config.Load(configPath)
	if len(cfg.Cameras) != 1 {
		t.Errorf("expected 1 camera in config, got %d", len(cfg.Cameras))
	}

	// setupDone should be signaled
	select {
	case <-setupDone:
	default:
		t.Error("setupDone channel should be signaled")
	}
}
```

- [ ] **Step 6: Implement setup handlers**

Create `internal/api/setup.go`:

```go
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rvben/vedetta/internal/camera"
	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// SetupHandler serves the onboarding endpoints during setup mode.
type SetupHandler struct {
	configPath string
	db         *storage.DB
	setupDone  chan struct{}

	mu         sync.Mutex
	thumbnails map[string][]byte // IP → JPEG
	completed  bool
}

func NewSetupHandler(configPath string, db *storage.DB, setupDone chan struct{}) *SetupHandler {
	return &SetupHandler{
		configPath: configPath,
		db:         db,
		setupDone:  setupDone,
		thumbnails: make(map[string][]byte),
	}
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleSetup creates the admin account and writes the initial config.
func (h *SetupHandler) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}
	passwordHash := string(hash)

	// Save to DB (always works)
	if err := h.db.SaveAuthUser(req.Username, passwordHash); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save user"})
		return
	}

	// Try to write config file
	err = config.WriteInitialConfig(h.configPath, req.Username, passwordHash)
	if err != nil {
		// Read-only fallback: return YAML for manual save
		slog.Warn("config path not writable, returning YAML", "path", h.configPath, "error", err)
		yaml, genErr := config.GenerateInitialConfigYAML(req.Username, passwordHash)
		if genErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate config"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "config_readonly",
			"config_yaml": yaml,
			"message":     "Config path is not writable. Save this YAML as your config file.",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleDiscover runs ONVIF WS-Discovery and returns found cameras.
func (h *SetupHandler) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	cameras, err := camera.DiscoverCameras(5 * time.Second)
	if err != nil {
		slog.Error("discovery failed", "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{"cameras": []interface{}{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"cameras": cameras})
}

type probeRequest struct {
	Cameras  []probeCamera `json:"cameras"`
	Username string        `json:"username"`
	Password string        `json:"password"`
}

type probeCamera struct {
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Manufacturer string `json:"manufacturer"`
}

type probeResult struct {
	IP        string                 `json:"ip"`
	Status    string                 `json:"status"`
	Streams   []camera.StreamProfile `json:"streams,omitempty"`
	Thumbnail string                 `json:"thumbnail,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// HandleProbe tests RTSP credentials against discovered cameras.
func (h *SetupHandler) HandleProbe(w http.ResponseWriter, r *http.Request) {
	var req probeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	var results []probeResult
	for _, cam := range req.Cameras {
		port := cam.Port
		if port == 0 {
			port = 554
		}
		profiles, err := camera.ProbeRTSPWithCredentials(
			cam.IP, port, cam.Manufacturer, req.Username, req.Password,
		)
		if err != nil || len(profiles) == 0 {
			errMsg := "no streams found"
			if err != nil {
				errMsg = err.Error()
			}
			results = append(results, probeResult{
				IP:     cam.IP,
				Status: "auth_failed",
				Error:  errMsg,
			})
			continue
		}
		results = append(results, probeResult{
			IP:        cam.IP,
			Status:    "ok",
			Streams:   profiles,
			Thumbnail: "/api/discover/thumbnail/" + cam.IP,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// HandleThumbnail serves a cached JPEG thumbnail for a probed camera.
func (h *SetupHandler) HandleThumbnail(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	h.mu.Lock()
	data, ok := h.thumbnails[ip]
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(data)
}

type addCamerasRequest struct {
	Cameras []addCameraEntry `json:"cameras"`
}

type addCameraEntry struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	RecordURL string `json:"record_url"`
}

// HandleAddCameras appends cameras to the config file and signals setup complete.
func (h *SetupHandler) HandleAddCameras(w http.ResponseWriter, r *http.Request) {
	var req addCamerasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	var yamlSnippets []string
	for _, entry := range req.Cameras {
		cam := config.CameraConfig{
			Name:      entry.Name,
			URL:       entry.URL,
			RecordURL: entry.RecordURL,
			Detect:    config.DetectStreamConfig{Width: 640, Height: 480, FPS: 5},
			Record:    config.StreamConfig{Width: 1920, Height: 1080, FPS: 15},
		}
		err := config.AppendCamera(h.configPath, cam, "")
		if err != nil {
			// Read-only fallback
			yaml, _ := config.GenerateCameraYAML(cam, "")
			yamlSnippets = append(yamlSnippets, yaml)
		}
	}

	h.signalComplete()

	if len(yamlSnippets) > 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":      "config_readonly",
			"config_yaml": yamlSnippets,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleComplete signals setup is done (used by "Skip for Now").
func (h *SetupHandler) HandleComplete(w http.ResponseWriter, r *http.Request) {
	h.signalComplete()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SetupHandler) signalComplete() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.completed && h.setupDone != nil {
		h.completed = true
		close(h.setupDone)
	}
}
```

- [ ] **Step 7: Add ProbeRTSPWithCredentials to camera/onvif.go**

This wraps existing `ProbeRTSPForBrand` but injects credentials into the RTSP URLs:

```go
// ProbeRTSPWithCredentials probes RTSP streams with the given credentials.
func ProbeRTSPWithCredentials(ip string, port int, manufacturer, username, password string) ([]StreamProfile, error) {
	profiles, err := ProbeRTSPForBrand(ip, port, manufacturer)
	if err != nil {
		return nil, err
	}

	// Inject credentials into discovered URLs
	var authed []StreamProfile
	for _, p := range profiles {
		u, err := url.Parse(p.URL)
		if err != nil {
			continue
		}
		u.User = url.UserPassword(username, password)
		// Re-test with credentials
		if testRTSPURL(u.String()) {
			authed = append(authed, StreamProfile{URL: u.String(), Resolution: p.Resolution})
		}
	}

	if len(authed) == 0 && len(profiles) > 0 {
		return nil, fmt.Errorf("authentication failed")
	}
	return authed, nil
}
```

- [ ] **Step 8: Run all tests**

Run: `go test ./internal/api/ -run TestSetup -v && go test ./internal/camera/ -v`
Expected: All tests pass

- [ ] **Step 9: Commit**

```bash
git add internal/api/setup.go internal/api/setup_test.go internal/camera/onvif.go
git commit -m "feat(api): add setup mode handlers for onboarding"
```

---

## Task 7: Server Setup Mode Routing

**Files:**
- Modify: `internal/api/server.go:78-222` (route registration)
- Modify: `internal/api/auth.go:65-78` (public paths)

- [ ] **Step 1: Add setup mode to Server struct**

In `internal/api/server.go`, add to Server struct:

```go
setupHandler *SetupHandler
setupMode    bool
```

- [ ] **Step 2: Add setup mode constructor**

```go
// NewSetupMode creates a server that only serves onboarding endpoints.
func NewSetupMode(cfg config.APIConfig, db *storage.DB, configPath string, setupDone chan struct{}) *Server {
	s := &Server{
		config:    cfg,
		db:        db,
		mux:       http.NewServeMux(),
		sseClients: make(map[chan []byte]struct{}),
		setupMode:  true,
	}

	sh := NewSetupHandler(configPath, db, setupDone)
	s.setupHandler = sh

	// Setup-only routes (no auth)
	s.mux.HandleFunc("POST /api/setup", sh.HandleSetup)
	s.mux.HandleFunc("GET /api/discover", sh.HandleDiscover)
	s.mux.HandleFunc("POST /api/discover/probe", sh.HandleProbe)
	s.mux.HandleFunc("GET /api/discover/thumbnail/{ip}", sh.HandleThumbnail)
	s.mux.HandleFunc("POST /api/cameras", sh.HandleAddCameras)
	s.mux.HandleFunc("POST /api/setup/complete", sh.HandleComplete)
	s.mux.HandleFunc("GET /api/setup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "setup"})
	})

	// Static files (serve setup.html as the default page)
	staticSub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(staticSub))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			r.URL.Path = "/setup.html"
		}
		fileServer.ServeHTTP(w, r)
	})

	// All other API routes return 403
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "setup not complete"})
	})

	return s
}
```

- [ ] **Step 3: Modify Start() to skip auth middleware in setup mode**

In the `Start()` method, conditionally apply middleware:

Modify `Start()` (line 224-245) to conditionally apply middleware. Note: `authMiddleware` is a package-level function with signature `authMiddleware(s *Server, next http.Handler) http.Handler` (not a method):

```go
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var handler http.Handler = s.mux
	if !s.setupMode {
		handler = s.readyMiddleware(authMiddleware(s, s.mux))
	}

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// ... rest of Start() unchanged (TLS handling, ListenAndServe)
}
```

- [ ] **Step 4: Add method to transition from setup to full mode**

```go
// TransitionToFull replaces the setup-mode mux with the full route set.
// Called after setup completes and subsystems are initialized.
func (s *Server) TransitionToFull(authChecker *auth.Checker) {
	s.auth = authChecker
	s.setupMode = false

	// Re-register all routes on a new mux
	newMux := http.NewServeMux()
	s.mux = newMux
	s.registerRoutes() // Extract current route registration into a method

	// Update the running server's handler.
	// Note: authMiddleware is a package-level function: authMiddleware(s, handler)
	s.httpSrv.Handler = s.readyMiddleware(authMiddleware(s, newMux))
}
```

Note: This requires extracting the route registration block (lines 127-219 of server.go) into a `registerRoutes()` method. This is a refactor of existing code — move the route registration into a callable method that both `New()` and `TransitionToFull()` can use.

- [ ] **Step 5: Run build and tests**

Run: `go build ./cmd/vedetta/ && go test ./internal/api/ -v`
Expected: Compiles and all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/auth.go
git commit -m "feat(api): add setup mode routing and transition to full mode"
```

---

## Task 8: Main Startup — Setup Mode Path

**Files:**
- Modify: `cmd/vedetta/main.go:34-58`

- [ ] **Step 1: Implement the setup mode startup path**

Replace the config loading section in `main()`:

```go
cfg, setupMode, err := config.LoadOrDefault(*configPath)
if err != nil {
	slog.Error("failed to load config", "error", err)
	os.Exit(1)
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// DB is needed in both setup and normal mode
dbPath := cfg.Storage.DBPath
if setupMode {
	dbPath = "./vedetta.db"
}
db, err := storage.New(dbPath)
if err != nil {
	slog.Error("failed to open database", "error", err)
	os.Exit(1)
}
defer func() { _ = db.Close() }()

if setupMode {
	slog.Info("no config file found, starting in setup mode", "config", *configPath)
	slog.Info("open the web UI to complete setup", "url", fmt.Sprintf("http://localhost:%d", cfg.API.Port))

	setupDone := make(chan struct{})
	server := api.NewSetupMode(cfg.API, db, *configPath, setupDone)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API server failed", "error", err)
			cancel()
		}
	}()

	// Wait for setup to complete or signal
	select {
	case <-setupDone:
		slog.Info("setup complete, loading config")
	case <-ctx.Done():
		return
	}

	// Reload the config that was just written.
	// If the config path was read-only (user was shown YAML to save manually),
	// the file won't exist. Fall back to defaults — auth is already in the DB.
	cfg, err = config.Load(*configPath)
	if err != nil {
		slog.Warn("config file not found after setup (read-only path?), using defaults", "error", err)
		cfg = config.Defaults()
	}

	// Seed auth users from config
	for _, user := range cfg.Auth.Users {
		_ = db.SeedAuthUser(user.Username, user.PasswordHash)
	}

	// Create auth checker from DB
	authChecker := auth.NewFromDB(cfg.API, db)
	defer authChecker.Close()

	// Continue to normal initialization below, reusing the running server
	// ... (all the subsystem initialization code)
	// At the end, transition the server:
	server.TransitionToFull(authChecker)
	server.SetSubsystems(manager, recorder, hub, ...)

	// ... rest of main (signal handling, etc.)
}

// Normal (non-setup) startup path continues here...
```

- [ ] **Step 2: Refactor main() to avoid duplicating init code**

Extract the subsystem initialization (lines 90-470 approximately) into a function that both the setup and normal paths call:

```go
func initSubsystems(ctx context.Context, cfg *config.Config, db *storage.DB, server *api.Server, authChecker *auth.Checker) (*camera.Manager, *recording.Recorder) {
	// ... detector, face recognizer, object embedder, hub, recorder,
	// camera registration, continuous recording, retention, MQTT,
	// zone sync, manager start, ONVIF subscribers, etc.
	// Returns manager and recorder for the event loop
}
```

This prevents code duplication between the two startup paths.

- [ ] **Step 3: Verify build and manual test**

Run: `go build ./cmd/vedetta/`
Then test manually:
1. Remove any existing config.yml
2. Run `./build/vedetta`
3. Verify "starting in setup mode" log message
4. Verify http://localhost:5050 serves setup.html

- [ ] **Step 4: Commit**

```bash
git add cmd/vedetta/main.go
git commit -m "feat: setup mode startup path in main"
```

---

## Task 9: Camera Manager — Hot-Add Support

**Files:**
- Modify: `internal/camera/manager.go:13-84`
- Add test in existing test file

- [ ] **Step 1: Write test for AddCamera**

```go
func TestManager_AddCamera(t *testing.T) {
	// NewManager signature (13 params): configs, detector, motion, events, eventEnds,
	// presenceEvents, hub, snapshotPath, snapshotQuality, recordingPath,
	// faceRecognizer, faceEvents, faceCropDir
	events := make(chan Event, 10)
	eventEnds := make(chan EventEnd, 10)
	presenceEvents := make(chan PresenceEvent, 10)
	faceEvents := make(chan FaceEvent, 10)
	m := NewManager(nil, nil, config.MotionConfig{}, events, eventEnds, presenceEvents, nil, "", 85, "", nil, faceEvents, "")
	if len(m.ListCameras()) != 0 {
		t.Fatal("expected 0 cameras initially")
	}

	cfg := config.CameraConfig{
		Name: "test_cam",
		URL:  "rtsp://localhost/stream",
	}
	m.AddCamera(cfg)

	names := m.ListCameras()
	if len(names) != 1 || names[0] != "test_cam" {
		t.Errorf("expected [test_cam], got %v", names)
	}
}
```

- [ ] **Step 2: Implement AddCamera**

In `internal/camera/manager.go`:

The Manager struct already has `mu sync.RWMutex` (line 20). However, it doesn't store the constructor params needed to create new cameras (motion config, channels, paths, etc.). Two options:

**Option A (recommended):** Store the constructor params in Manager. Add fields to the struct and populate them in `NewManager`. Then `AddCamera` can call `NewCamera` with the stored params.

**Option B:** `AddCamera` takes a pre-constructed `*Camera` instead of a config.

Go with Option A — it's cleaner for the API caller:

```go
// Add these fields to the Manager struct (after existing fields):
type Manager struct {
	cameras         map[string]*Camera
	order           []string
	detector        *detect.Detector
	events          chan<- Event
	hub             *rtsp.Hub
	mu              sync.RWMutex
	// Stored for hot-adding cameras:
	motionCfg       config.MotionConfig
	eventEnds       chan<- EventEnd
	presenceEvents  chan<- PresenceEvent
	snapshotPath    string
	snapshotQuality int
	recordingPath   string
	faceRecognizer  *detect.FaceRecognizer
	faceEvents      chan<- FaceEvent
	faceCropDir     string
}
```

Update `NewManager` to store these params (they're already passed as arguments, just not saved). Then:

```go
// AddCamera adds a new camera to the manager at runtime.
func (m *Manager) AddCamera(cfg config.CameraConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.cameras[cfg.Name]; exists {
		return
	}

	cam := NewCamera(cfg, m.detector, m.motionCfg, m.events, m.eventEnds, m.presenceEvents,
		m.hub, m.snapshotPath, m.snapshotQuality, m.recordingPath,
		m.faceRecognizer, m.faceEvents, m.faceCropDir)
	m.cameras[cfg.Name] = cam
	m.order = append(m.order, cfg.Name)
}

// StartCamera starts a specific camera's processing loop.
func (m *Manager) StartCamera(ctx context.Context, name string) {
	m.mu.RLock()
	cam, ok := m.cameras[name]
	m.mu.RUnlock()
	if !ok {
		return
	}
	go cam.Start(ctx)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/camera/ -run TestManager_AddCamera -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/camera/manager.go
git commit -m "feat(camera): add hot-add support to camera manager"
```

---

## Task 10: Onboarding UI — setup.html

**Files:**
- Create: `internal/api/static/setup.html`

- [ ] **Step 1: Create the setup page**

Create `internal/api/static/setup.html` — a single HTML file with two screens (account creation and camera discovery), using the existing design system (CSS variables, DM Sans, JetBrains Mono, Control Room Noir theme).

The file should:
- Import the existing `style.css` for design tokens
- Screen 1: centered card with username/password/confirm fields, vedetta logo, "Create Account & Continue" button
- Screen 2: auto-running ONVIF discovery with camera list, shared credentials field, per-camera override, thumbnails, "Add Selected" / "Skip for Now" buttons
- Transition between screens via JS (no page reload)
- Call the setup API endpoints (`/api/setup`, `/api/discover`, `/api/discover/probe`, `/api/cameras`, `/api/setup/complete`)
- Handle the read-only fallback (show YAML with copy button)
- Match the existing UI style exactly (use CSS variables from style.css)

This is a substantial HTML file. The implementation agent should reference `login.html` for the auth form styling pattern and `index.html` for the nav/layout pattern. The empty dashboard mockup from the brainstorming session (`.superpowers/brainstorm/*/empty-dashboard.html`) provides the visual reference.

- [ ] **Step 2: Verify it loads in the browser**

Run: `go build ./cmd/vedetta/ && ./build/vedetta` (with no config)
Open http://localhost:5050 — should show the setup page

- [ ] **Step 3: Test the full flow end-to-end**

1. Start with no config: `rm -f config.yml && ./build/vedetta`
2. Open http://localhost:5050 — see account creation screen
3. Create admin account → should advance to discovery screen
4. Discovery runs → shows any cameras on the network (or empty list)
5. Skip or add cameras → redirects to dashboard
6. Dashboard works — stats show, navigation works

- [ ] **Step 4: Commit**

```bash
git add internal/api/static/setup.html
git commit -m "feat(ui): add onboarding setup page with account creation and camera discovery"
```

---

## Task 11: Empty Dashboard State

**Files:**
- Modify: `internal/api/server.go` (camera-grid partial)
- Modify: `internal/api/static/app.js` (handle empty camera grid)

- [ ] **Step 1: Update the camera grid partial to handle zero cameras**

In the handler that serves `/partials/camera-grid` (in `server.go`), when there are no cameras, return an empty state HTML fragment instead of an empty grid:

```html
<div class="empty-hero">
  <div class="empty-icon">
    <svg>...</svg>
  </div>
  <div class="empty-title">No cameras configured yet</div>
  <div class="empty-desc">
    Vedetta can automatically discover cameras on your network using ONVIF.
    Or add one manually if you know the RTSP URL.
  </div>
  <div class="empty-actions">
    <a href="#" onclick="startDiscovery()" class="action-card primary">
      <div class="action-icon"><svg>...</svg></div>
      <div class="action-text"><h3>Discover Cameras</h3><p>Scan your network</p></div>
    </a>
    <a href="#" onclick="showAddManual()" class="action-card">
      <div class="action-icon"><svg>...</svg></div>
      <div class="action-text"><h3>Add Manually</h3><p>Enter RTSP URL</p></div>
    </a>
  </div>
</div>
```

- [ ] **Step 2: Add empty state CSS to style.css**

Add the `.empty-hero`, `.empty-icon`, `.empty-title`, `.empty-desc`, `.empty-actions`, `.action-card`, `.action-icon`, `.action-text` styles from the brainstorming mockup to the existing `style.css`.

- [ ] **Step 3: Wire up discover/add-manual JS in app.js**

Add `startDiscovery()` and `showAddManual()` functions that open a modal or navigate to a discovery view. These reuse the same discovery API endpoints (`/api/discover`, `/api/discover/probe`, `/api/cameras`) that the setup flow uses, but behind authentication.

- [ ] **Step 4: Test empty dashboard**

1. Start with a config that has auth but no cameras
2. Dashboard should show the empty state with discover/add CTAs
3. Click "Discover Cameras" → should trigger ONVIF scan
4. After adding a camera, the grid should update

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/static/style.css internal/api/static/app.js
git commit -m "feat(ui): empty dashboard state with discover and add-manual CTAs"
```

---

## Task 12: Integration Test

**Files:**
- Create: `internal/api/integration_test.go` (or add to `setup_test.go`)

- [ ] **Step 1: Write end-to-end setup flow test**

```go
func TestSetupFlow_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yml")
	dbPath := filepath.Join(dir, "test.db")

	db, err := storage.New(dbPath)
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	defer db.Close()

	setupDone := make(chan struct{})
	server := NewSetupMode(config.APIConfig{Host: "127.0.0.1", Port: 0}, db, configPath, setupDone)

	// Start on random port
	ts := httptest.NewServer(server.mux)
	defer ts.Close()

	// 1. Non-setup routes should be blocked
	resp, _ := http.Get(ts.URL + "/api/events")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for /api/events during setup, got %d", resp.StatusCode)
	}

	// 2. Create account
	body := strings.NewReader(`{"username":"admin","password":"test1234"}`)
	resp, _ = http.Post(ts.URL+"/api/setup", "application/json", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup failed: %d", resp.StatusCode)
	}

	// Verify config file exists
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// 3. Signal complete (skip cameras)
	resp, _ = http.Post(ts.URL+"/api/setup/complete", "application/json", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete failed: %d", resp.StatusCode)
	}

	// 4. setupDone should be closed
	select {
	case <-setupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("setupDone not signaled within 2s")
	}

	// 5. Config should be loadable
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config not loadable: %v", err)
	}
	if len(cfg.Auth.Users) != 1 {
		t.Errorf("expected 1 auth user, got %d", len(cfg.Auth.Users))
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./internal/api/ -run TestSetupFlow_EndToEnd -v`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/api/setup_test.go
git commit -m "test: add end-to-end setup flow integration test"
```
