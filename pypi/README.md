# Vedetta

Lightweight open-source network video recorder (NVR) with real-time object detection. Written in Go, ships as a single binary.

## Features

- YOLOv8 object detection via ONNX Runtime
- Continuous recording with configurable retention
- Motion detection with contour analysis
- WebRTC live streaming with MJPEG fallback
- Home Assistant integration via MQTT
- Web dashboard with timeline scrubber and event gallery
- Hardware acceleration (VAAPI, VideoToolbox, CUDA)
- Single binary, no Python dependency

## Installation

Vedetta is a Go application. This PyPI package is a placeholder for future `pip install vedetta` support.

For now, install via Docker or build from source:

```sh
# Docker
docker run -d --name vedetta -p 5050:5050 ghcr.io/rvben/vedetta:latest

# Build from source
git clone https://github.com/rvben/vedetta.git
cd vedetta && make build
```

See the [GitHub repository](https://github.com/rvben/vedetta) for full documentation.
