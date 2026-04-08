package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// yamlConfig mirrors Config but uses string durations for human-readable YAML output.
// time.Duration fields marshal as nanoseconds by default, so this struct ensures
// durations like "10m" and "5s" appear in the generated YAML.
type yamlConfig struct {
	Auth      yamlAuth      `yaml:"auth"`
	API       APIConfig     `yaml:"api"`
	Storage   StorageConfig `yaml:"storage"`
	Recording yamlRecording `yaml:"recording"`
	Events    EventConfig   `yaml:"events"`
	Detect    yamlDetect    `yaml:"detect"`
}

type yamlAuth struct {
	Users []AuthUser `yaml:"users"`
}

type yamlRecording struct {
	Path          string `yaml:"path"`
	Continuous    bool   `yaml:"continuous"`
	SegmentLength string `yaml:"segment_length"`
	PreCapture    string `yaml:"pre_capture"`
	PostCapture   string `yaml:"post_capture"`
	RetainDays    int    `yaml:"retain_days"`
	EventRetain   int    `yaml:"event_retain_days"`
}

type yamlDetect struct {
	ScoreThreshold float32 `yaml:"score_threshold"`
}

// WriteInitialConfig writes a new config.yml with auth credentials and all defaults.
func WriteInitialConfig(path, username, passwordHash string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checking config: %w", err)
	}

	content, err := GenerateInitialConfigYAML(username, passwordHash)
	if err != nil {
		return fmt.Errorf("generating config: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// GenerateInitialConfigYAML returns the YAML string for an initial config with
// auth credentials and default values. The output is loadable by Load().
func GenerateInitialConfigYAML(username, passwordHash string) (string, error) {
	cfg := yamlConfig{
		Auth: yamlAuth{
			Users: []AuthUser{
				{Username: username, PasswordHash: passwordHash},
			},
		},
		API: APIConfig{
			Host:     "0.0.0.0",
			Port:     5050,
			Exposure: "lan",
		},
		Storage: StorageConfig{
			DBPath: "./vedetta.db",
		},
		Recording: yamlRecording{
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
		Detect: yamlDetect{
			ScoreThreshold: 0.65,
		},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("closing encoder: %w", err)
	}
	return buf.String(), nil
}

// AppendCamera adds a camera to an existing config file using yaml.Node to
// preserve the existing document structure (comments, ordering, other sections).
func AppendCamera(path string, cam CameraConfig, comment string) error {
	if err := ValidateCameraName(cam.Name); err != nil {
		return fmt.Errorf("invalid camera name: %w", err)
	}
	if cam.URL == "" {
		return fmt.Errorf("camera url is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// doc is a Document node; its first Content is the root mapping
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return fmt.Errorf("unexpected YAML structure: expected document node")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("unexpected YAML structure: expected mapping node")
	}

	// Find or create the "cameras" key in the root mapping
	var camerasSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "cameras" {
			camerasSeq = root.Content[i+1]
			break
		}
	}

	if camerasSeq == nil {
		// Create "cameras" key and empty sequence
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: "cameras",
		}
		seqNode := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		root.Content = append(root.Content, keyNode, seqNode)
		camerasSeq = seqNode
	}

	// Marshal the camera to a yaml.Node
	camNode, err := marshalCameraNode(cam, comment)
	if err != nil {
		return fmt.Errorf("marshaling camera: %w", err)
	}

	camerasSeq.Content = append(camerasSeq.Content, camNode)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("closing encoder: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}

	return os.WriteFile(path, buf.Bytes(), info.Mode().Perm())
}

// GenerateCameraYAML returns a YAML snippet for a camera configuration.
func GenerateCameraYAML(cam CameraConfig, comment string) (string, error) {
	if err := ValidateCameraName(cam.Name); err != nil {
		return "", fmt.Errorf("invalid camera name: %w", err)
	}
	if cam.URL == "" {
		return "", fmt.Errorf("camera url is required")
	}

	camNode, err := marshalCameraNode(cam, comment)
	if err != nil {
		return "", fmt.Errorf("marshaling camera: %w", err)
	}

	// Wrap in a sequence for proper YAML list output
	seqNode := &yaml.Node{
		Kind:    yaml.SequenceNode,
		Tag:     "!!seq",
		Content: []*yaml.Node{camNode},
	}

	// Wrap in a mapping with "cameras" key
	mapNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "cameras"},
			seqNode,
		},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(mapNode); err != nil {
		return "", fmt.Errorf("encoding camera: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("closing encoder: %w", err)
	}
	return buf.String(), nil
}

// marshalCameraNode creates a yaml.Node for a CameraConfig, adding a head
// comment if provided.
func marshalCameraNode(cam CameraConfig, comment string) (*yaml.Node, error) {
	// Build a minimal struct with only set fields for cleaner YAML
	camYAML := struct {
		Name      string `yaml:"name"`
		URL       string `yaml:"url"`
		RecordURL string `yaml:"record_url,omitempty"`
	}{
		Name:      cam.Name,
		URL:       cam.URL,
		RecordURL: cam.RecordURL,
	}

	var camNode yaml.Node
	if err := camNode.Encode(camYAML); err != nil {
		return nil, err
	}

	if comment != "" {
		camNode.HeadComment = comment
	}

	return &camNode, nil
}
