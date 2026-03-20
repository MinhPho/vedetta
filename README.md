# Vedetta

Vedetta is an open-source Network Video Recorder (NVR) written in Go. Inspired by [Frigate](https://frigate.video), it ships as a single binary with no Python dependency.

## Features

- **Object detection** -- YOLOv8 via ONNX Runtime (pure-Go or C backend)
- **Continuous recording** -- segment-based with configurable retention
- **Event clips** -- pre/post capture around detected objects
- **Motion detection** -- contour-based; YOLO runs only when motion is detected
- **Object tracking** -- Hungarian algorithm across frames
- **Live streaming** -- WebRTC with MJPEG fallback
- **Web dashboard** -- dark theme, htmx + vanilla JS, no build step
- **Home Assistant** -- MQTT integration with auto-discovery
- **ONVIF discovery** -- find cameras on the network (`vedetta discover`)
- **Hardware acceleration** -- VAAPI, VideoToolbox, CUDA (auto-detected)
- **Per-camera zones** -- filter which objects matter in each zone
- **Storage management** -- max storage cap, automatic cleanup
- **SQLite** -- WAL-mode database, embedded in the binary
- **Single binary** -- static files embedded with `go:embed`

## Quick Start

### Binary

```sh
# Build from source
make build

# Run with a config file
./build/vedetta -config config.yml
```

### Docker

```sh
docker run -d \
  --name vedetta \
  -v /path/to/config.yml:/config.yml \
  -v /path/to/recordings:/recordings \
  -p 5050:5050 \
  ghcr.io/rvben/vedetta:latest
```

## Configuration

Vedetta is configured with a single YAML file. See [`config.example.yml`](config.example.yml) for a complete example.

### Cameras

```yaml
cameras:
  - name: front_door
    url: rtsp://user:pass@192.168.1.100:554/stream1
    record_url: rtsp://user:pass@192.168.1.100:554/stream0  # optional high-res stream
    enabled: true
    detect:
      width: 640
      height: 480
      fps: 5
    record:
      width: 1920
      height: 1080
      fps: 15
    zones:
      - name: driveway
        coordinates: [0.1, 0.5, 0.9, 0.5, 0.9, 1.0, 0.1, 1.0]
        objects: [person, car]
```

Each camera has two optional streams: `url` for the detection stream (lower resolution, less CPU) and `record_url` for recording (full resolution). If `record_url` is omitted, `url` is used for both.

Zones are defined as normalized coordinates (0.0--1.0) and can filter which object classes trigger events.

### Detection

```yaml
detect:
  model_path: ""            # path to YOLOv8 ONNX model
  score_threshold: 0.5      # minimum confidence score
  motion_threshold: 0.02    # fraction of pixels that must change
```

### Recording

```yaml
recording:
  path: ./recordings
  continuous: true           # record continuously, not just events
  segment_length: 10m        # length of each continuous segment
  pre_capture: 5s            # seconds before event to include in clip
  post_capture: 10s          # seconds after event to include in clip
  retain_days: 7             # delete continuous segments after N days
  event_retain_days: 30      # keep event clips longer
```

### Storage

```yaml
storage:
  db_path: ./vedetta.db
```

### MQTT

```yaml
mqtt:
  enabled: false
  host: 127.0.0.1
  port: 1883
  topic: vedetta
```

### API

```yaml
api:
  host: 0.0.0.0
  port: 5050
```

## Camera Setup

Common RTSP URL formats for popular camera brands:

| Brand | Main Stream | Sub Stream |
|-------|------------|------------|
| **Tapo** | `rtsp://user:pass@IP:554/stream1` | `rtsp://user:pass@IP:554/stream2` |
| **Reolink** | `rtsp://user:pass@IP:554/h264Preview_01_main` | `rtsp://user:pass@IP:554/h264Preview_01_sub` |
| **Hikvision** | `rtsp://user:pass@IP:554/Streaming/Channels/101` | `rtsp://user:pass@IP:554/Streaming/Channels/102` |
| **Dahua** | `rtsp://user:pass@IP:554/cam/realmonitor?channel=1&subtype=0` | `rtsp://user:pass@IP:554/cam/realmonitor?channel=1&subtype=1` |

Use `vedetta discover` to scan the local network for ONVIF-compatible cameras and print their RTSP URLs.

## MQTT / Home Assistant

When MQTT is enabled, Vedetta publishes Home Assistant auto-discovery messages so cameras and sensors appear automatically.

The default topic prefix is `vedetta`. Messages are published under:

- `vedetta/<camera>/detection` -- object detection events
- `homeassistant/binary_sensor/vedetta_<camera>/config` -- auto-discovery

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/system` | System status (CPU, memory, storage) |
| `GET` | `/api/cameras` | List all cameras and their status |
| `GET` | `/api/cameras/{name}/snapshot` | Current JPEG snapshot from camera |
| `GET` | `/api/cameras/{name}/mjpeg` | MJPEG live stream |
| `POST` | `/api/cameras/{name}/webrtc/offer` | WebRTC signaling (SDP offer/answer) |
| `GET` | `/api/events` | List recorded events |
| `GET` | `/api/events/{id}` | Get single event details |
| `GET` | `/api/events/{id}/snapshot` | Event thumbnail |
| `GET` | `/api/events/{id}/clip` | Download event video clip |

The web dashboard is served at `/` and uses htmx partials for dynamic updates.

## Development

Prerequisites: Go 1.22+, ffmpeg.

```sh
make build          # build the binary
make build-capi     # build with C ONNX Runtime backend
make test           # run tests
make bench          # run detection benchmarks
make lint           # run golangci-lint
make fmt            # format code
make check          # lint + test
make clean          # remove build artifacts
```

## Architecture

```
RTSP Camera
    |
    v
  ffmpeg (decode) ──> Motion Detector
                          |
                     (motion detected)
                          |
                          v
                    YOLOv8 Detector ──> Object Tracker (Hungarian)
                          |                    |
                          v                    v
                    Event Manager         MQTT Publisher
                          |
                    +-----+-----+
                    |           |
              Event Clips   Continuous
                            Segments
```

Frames flow from camera through motion detection. YOLO only runs when motion is detected, keeping CPU usage low. Detected objects are tracked across frames with the Hungarian algorithm to maintain identity. Events trigger clip extraction from the continuous recording buffer.
