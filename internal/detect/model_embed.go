package detect

// embeddedModel can be set at build time by replacing this file with one that
// uses go:embed. When nil, the model is auto-downloaded on first run.
//
// To embed the model at build time:
//  1. Place yolov8n.onnx in internal/detect/models/
//  2. Build with: go build -tags embed_model ./...
var embeddedModel []byte
