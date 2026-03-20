package mqtt

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rvben/vedetta/internal/camera"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"front-door", "front_door"},
		{"Back Yard", "back_yard"},
		{"garage.cam", "garage_cam"},
		{"simple", "simple"},
		{"UPPER", "upper"},
		{"front-door.cam 1", "front_door_cam_1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEventTopicFormat(t *testing.T) {
	baseTopic := "vedetta"
	cameraName := "front-door"

	got := fmt.Sprintf("%s/events/%s", baseTopic, cameraName)
	want := "vedetta/events/front-door"
	if got != want {
		t.Errorf("event topic = %q, want %q", got, want)
	}
}

func TestCameraStatusTopicFormat(t *testing.T) {
	baseTopic := "vedetta"
	cameraName := "backyard"

	got := fmt.Sprintf("%s/camera/%s/status", baseTopic, cameraName)
	want := "vedetta/camera/backyard/status"
	if got != want {
		t.Errorf("status topic = %q, want %q", got, want)
	}
}

func TestEventPayloadSerialization(t *testing.T) {
	event := camera.Event{
		ID:         "front-t1-1234",
		CameraName: "front",
		Label:      "person",
		Score:      0.95,
		Box:        [4]int{100, 200, 300, 400},
		Timestamp:  time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if decoded["camera"] != "front" {
		t.Errorf("camera = %v, want %q", decoded["camera"], "front")
	}
	if decoded["label"] != "person" {
		t.Errorf("label = %v, want %q", decoded["label"], "person")
	}
	if decoded["id"] != "front-t1-1234" {
		t.Errorf("id = %v, want %q", decoded["id"], "front-t1-1234")
	}
}

func TestDiscoveryBinarySensorPayload(t *testing.T) {
	cameraName := "front-door"
	baseTopic := "vedetta"
	objectID := fmt.Sprintf("vedetta_%s", sanitizeName(cameraName))

	device := haDevice{
		Identifiers:  []string{"vedetta_" + sanitizeName(cameraName)},
		Name:         "Vedetta " + cameraName,
		Manufacturer: "Vedetta",
		Model:        "NVR",
	}

	config := haBinarySensorConfig{
		Name:              cameraName,
		UniqueID:          objectID + "_status",
		StateTopic:        fmt.Sprintf("%s/camera/%s/status", baseTopic, cameraName),
		AvailabilityTopic: baseTopic + "/availability",
		DeviceClass:       "connectivity",
		PayloadOn:         "ON",
		PayloadOff:        "OFF",
		Device:            device,
	}

	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal discovery: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal discovery: %v", err)
	}

	if decoded["unique_id"] != "vedetta_front_door_status" {
		t.Errorf("unique_id = %v, want %q", decoded["unique_id"], "vedetta_front_door_status")
	}
	if decoded["state_topic"] != "vedetta/camera/front-door/status" {
		t.Errorf("state_topic = %v, want %q", decoded["state_topic"], "vedetta/camera/front-door/status")
	}
	if decoded["availability_topic"] != "vedetta/availability" {
		t.Errorf("availability_topic = %v, want %q", decoded["availability_topic"], "vedetta/availability")
	}
	if decoded["device_class"] != "connectivity" {
		t.Errorf("device_class = %v, want %q", decoded["device_class"], "connectivity")
	}
	if decoded["payload_on"] != "ON" {
		t.Errorf("payload_on = %v, want %q", decoded["payload_on"], "ON")
	}

	deviceMap, ok := decoded["device"].(map[string]any)
	if !ok {
		t.Fatal("device field is not a map")
	}
	if deviceMap["manufacturer"] != "Vedetta" {
		t.Errorf("manufacturer = %v, want %q", deviceMap["manufacturer"], "Vedetta")
	}
	if deviceMap["model"] != "NVR" {
		t.Errorf("model = %v, want %q", deviceMap["model"], "NVR")
	}
}

func TestDiscoveryDeviceTriggerPayload(t *testing.T) {
	cameraName := "garage"
	baseTopic := "myhome"

	device := haDevice{
		Identifiers:  []string{"vedetta_" + sanitizeName(cameraName)},
		Name:         "Vedetta " + cameraName,
		Manufacturer: "Vedetta",
		Model:        "NVR",
	}

	config := haDeviceTriggerConfig{
		AutomationType: "trigger",
		Type:           "detection",
		Subtype:        "object_detected",
		Topic:          fmt.Sprintf("%s/events/%s", baseTopic, cameraName),
		Device:         device,
	}

	payload, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal trigger: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal trigger: %v", err)
	}

	if decoded["automation_type"] != "trigger" {
		t.Errorf("automation_type = %v, want %q", decoded["automation_type"], "trigger")
	}
	if decoded["type"] != "detection" {
		t.Errorf("type = %v, want %q", decoded["type"], "detection")
	}
	if decoded["topic"] != "myhome/events/garage" {
		t.Errorf("topic = %v, want %q", decoded["topic"], "myhome/events/garage")
	}
}

func TestDiscoveryTopicFormat(t *testing.T) {
	cameraName := "front-door"
	objectID := fmt.Sprintf("vedetta_%s", sanitizeName(cameraName))

	sensorTopic := fmt.Sprintf("homeassistant/binary_sensor/%s/config", objectID)
	if sensorTopic != "homeassistant/binary_sensor/vedetta_front_door/config" {
		t.Errorf("sensor topic = %q", sensorTopic)
	}

	triggerTopic := fmt.Sprintf("homeassistant/device_automation/%s_detection/config", objectID)
	if triggerTopic != "homeassistant/device_automation/vedetta_front_door_detection/config" {
		t.Errorf("trigger topic = %q", triggerTopic)
	}
}

func TestAvailabilityTopicFormat(t *testing.T) {
	baseTopic := "vedetta"
	got := baseTopic + "/availability"
	if got != "vedetta/availability" {
		t.Errorf("availability topic = %q", got)
	}

	baseTopic = "custom/prefix"
	got = baseTopic + "/availability"
	if got != "custom/prefix/availability" {
		t.Errorf("availability topic = %q", got)
	}
}

func TestEventPayloadWithOptionalFields(t *testing.T) {
	event := camera.Event{
		ID:           "cam1-t2-5678",
		CameraName:   "cam1",
		Label:        "car",
		Score:        0.87,
		Box:          [4]int{0, 0, 640, 480},
		Timestamp:    time.Now(),
		SnapshotPath: "/snapshots/cam1/123.jpg",
		ClipPath:     "/recordings/cam1/clip_123.mp4",
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if decoded["snapshot_path"] != "/snapshots/cam1/123.jpg" {
		t.Errorf("snapshot_path = %v", decoded["snapshot_path"])
	}
	if decoded["clip_path"] != "/recordings/cam1/clip_123.mp4" {
		t.Errorf("clip_path = %v", decoded["clip_path"])
	}
}

func TestEventPayloadOmitsEmptyOptionalFields(t *testing.T) {
	event := camera.Event{
		ID:         "cam1-t1-1234",
		CameraName: "cam1",
		Label:      "person",
		Score:      0.9,
		Box:        [4]int{10, 20, 30, 40},
		Timestamp:  time.Now(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if _, exists := decoded["snapshot_path"]; exists {
		t.Error("snapshot_path should be omitted when empty")
	}
	if _, exists := decoded["clip_path"]; exists {
		t.Error("clip_path should be omitted when empty")
	}
}
