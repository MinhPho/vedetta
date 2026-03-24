# Web-Based Onboarding Design

## Summary

Vedetta should start without a config file and guide the user through setup via the web UI. The binary enters "setup mode" when no config is found, serves an onboarding flow that creates an admin account and optionally discovers cameras, writes a `config.yml`, then transitions to normal operation without restart.

## Goals

- Zero-config first run: `./vedetta` or `docker run` with no arguments gets you to a working web UI
- Time to value: user sees live camera feeds within 2 minutes of first launch
- Config file remains the source of truth for infrastructure (cameras, paths, detection settings)
- Works identically for binary and Docker installations

## Non-Goals

- Config editor UI for all settings (future work)
- Multi-user onboarding (single admin account created during setup)
- Automatic TLS provisioning during setup

---

## Architecture

### Startup Flow

`main.go` uses a new `config.LoadOrDefault(path)` function:

- **File exists and valid:** Normal boot. No changes to current behavior.
- **File exists but invalid:** Exit with error. Do not silently enter setup mode on a broken config.
- **File missing:** Enter setup mode. Return a default config with sensible defaults and `setupMode=true`.

```
cfg, setupMode, err := config.LoadOrDefault(*configPath)

if setupMode:
    start DB (default path ./vedetta.db)
    start API server in setup mode
    block on setupComplete channel
    reload config from written file
    fall through to normal init

normal init:
    seed auth users from config → DB
    start all subsystems
    wire subsystems into API server
    switch API to full route set
```

The API server is created once and stays running throughout. It swaps from setup routes to full routes via the existing `SetSubsystems()` pattern.

### Setup Mode Routes

Only these endpoints are available during setup:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Onboarding UI (setup.html) |
| POST | `/api/setup` | Create admin account, write initial config |
| GET | `/api/discover` | Run ONVIF WS-Discovery |
| POST | `/api/discover/probe` | Probe cameras with credentials, return streams + thumbnails |
| GET | `/api/discover/thumbnail/:ip` | Serve cached JPEG thumbnail for a probed camera |
| POST | `/api/cameras` | Add selected cameras to config |
| POST | `/api/setup/complete` | Signal setup complete (used by "Skip for Now") |
| GET | `/api/setup/status` | Current setup state |

All other routes return 302 to `/` (HTML) or 403 (API). No authentication is required during setup because no credentials exist yet.

### Security During Setup

Setup mode is unauthenticated by necessity. Mitigations:

- Once `POST /api/setup` completes and config is written, setup endpoints are permanently disabled for the process lifetime.
- A config file on disk means setup mode is never entered on subsequent starts.
- Credentials are sent over the LAN connection and hashed server-side with bcrypt (same trust model as router setup pages).

---

## Config File Management

### Source of Truth

The config file (`config.yml`) is the source of truth for infrastructure settings: cameras, storage paths, detection parameters, recording settings, API exposure, MQTT.

Auth users in config serve as a seed/recovery mechanism (see Auth section below).

### Generation

**Step 1 — Account creation writes the initial file:**

```yaml
auth:
  users:
    - username: admin
      password_hash: "$2a$10$..."

api:
  host: 0.0.0.0
  port: 5050
  exposure: lan

storage:
  db_path: ./vedetta.db

recording:
  path: ./recordings
  continuous: true
  segment_length: 10m
  pre_capture: 5s
  post_capture: 10s
  retain_days: 7
  event_retain_days: 30

events:
  cooldown_seconds: 30
  retain_days: 90
  snapshot_path: ./snapshots
  snapshot_quality: 85

detect:
  score_threshold: 0.65
```

All defaults match the code defaults in `config.go` and are spelled out so the file is self-documenting. No cameras section yet.

**Step 2 — Adding cameras appends to the file:**

Uses `yaml.Node`-based insertion to preserve existing structure and comments. Parse file as `yaml.Node` tree, find or create the `cameras` sequence, append new camera nodes.

Each camera entry includes a comment noting the discovered manufacturer/model/IP.

**Read-only fallback:** If the config path is not writable (detected at write time), the API returns the generated YAML in the response body. The UI displays it with a copy button and instructions to save manually. No hard stops.

### Validation Changes

- Remove the "at least one camera" hard requirement from `config.Load()`. Zero cameras is valid.
- Keep "at least one auth user" for the normal boot path (skipped in setup mode).
- New function `config.LoadOrDefault(path) (*Config, bool, error)` handles the missing-file case.

---

## Auth: DB-Primary with Config Seed

Auth users live in the database, with config as a seed/recovery mechanism. Same pattern as zones.

- During onboarding, the admin user is created in the DB. A corresponding `auth.users` entry is written to config as a recovery fallback.
- On normal startup, `auth.users` from config are seeded into the DB (inserted if not already present, never overwritten). Mirrors `syncConfigZones`.
- Password changes, new users, user deletion — all happen through the DB via the API.
- Recovery: if locked out, add a user to `config.yml` and restart. The seed path restores access.
- The existing `auth.ValidateConfig()` call in `main.go` is replaced by the DB-based auth check. Config auth validation only applies to the seed path (ensuring seeded users have username + password_hash).

---

## Onboarding UI

Two screens served from `setup.html`, embedded via `go:embed`. Uses the existing design system (DM Sans, JetBrains Mono, Control Room Noir theme).

### Screen 1: Create Admin Account

Centered card, vedetta logo, three fields: username, password, confirm password. "Create Account & Continue" button.

On submit: `POST /api/setup` with `{username, password}`. Server hashes with bcrypt, writes config (or returns YAML if read-only). UI transitions to screen 2.

### Screen 2: Camera Discovery

ONVIF scan starts automatically on load (`GET /api/discover`). Pulsing animation during scan.

Camera cards appear showing: name, manufacturer, model, IP, status indicator.

**Credential flow:**
- Single default credential row (username + password) at the top, applied to all cameras.
- When credentials are entered, `POST /api/discover/probe` fires for each camera.
- Cameras that succeed: green checkmark, RTSP stream URLs, live JPEG thumbnail from the actual camera feed.
- Cameras that fail: red indicator, inline "Use different credentials" link that expands per-camera fields.

User checks cameras to add. Two actions:
- "Add Selected Cameras" (primary button) → `POST /api/cameras`
- "Skip for Now" (text link) → proceed to dashboard

The sub stream is assigned to `url` (detect) and main stream to `record_url` automatically based on probe results.

Camera credentials are embedded in RTSP URLs in plaintext within the config file. This matches the existing config model (`rtsp://user:pass@host/path`) and is standard practice for NVR software.

### Empty Dashboard (Post-Onboarding)

If no cameras were added, the dashboard shows the real UI shell (nav, stats row, page header) with an empty state in the camera grid area:

- Dashed-border container with camera icon (pulsing ring animation)
- "No cameras configured yet" heading
- Two CTAs: "Discover Cameras" (primary, triggers the same discovery flow) and "Add Manually" (secondary)
- System readiness strip showing DB, detection model, and storage status

Stats row shows live system data: 0 cameras, 0 online, 0 events, available storage.

---

## Discovery & Probing API

### `GET /api/discover`

Runs `camera.DiscoverCameras()` (existing function). Returns discovered cameras as JSON with IP, name, manufacturer, model, port.

Available in both setup mode (unauthenticated) and normal mode (authenticated, for the dashboard's "Discover Cameras" button).

### `POST /api/discover/probe`

Accepts a list of camera IPs with shared credentials. For each camera:

1. Injects credentials into RTSP URLs
2. Probes using `camera.ProbeRTSPForBrand()` (manufacturer-aware stream path matching)
3. On success, pulls a single JPEG frame from the first working stream
4. Returns status, discovered stream URLs, and a thumbnail endpoint per camera

Thumbnails are held in memory (map of IP → JPEG bytes) and served via `GET /api/discover/thumbnail/:ip`.

Available in both setup mode and normal mode (authenticated).

### `POST /api/cameras`

Accepts selected cameras with validated RTSP URLs. Writes to config using `yaml.Node` insertion. If read-only, returns YAML snippet in response.

In setup mode: signals `setupComplete` after writing, triggering subsystem initialization.
In normal mode (authenticated): writes to config, then hot-loads the new camera into the running system (start RTSP source, register with recorder, add to manager) without restart.

### Post-Setup Availability

All three discovery endpoints (`/api/discover`, `/api/discover/probe`, `/api/cameras`) are available in normal mode behind authentication. This powers the dashboard's "Discover Cameras" and "Add Manually" buttons in the empty state, giving the same camera-add experience post-onboarding.

---

## Main Goroutine Transition

The onboarding is a two-screen sequential flow. The transition to running mode happens at the end of the flow, not after each screen:

1. Screen 1: `POST /api/setup` creates the admin account and writes the initial config. The server responds with success but does **not** signal `setupComplete` yet. The UI advances to screen 2.
2. Screen 2: The user either clicks "Add Selected Cameras" (`POST /api/cameras` writes cameras to config and signals `setupComplete`) or clicks "Skip for Now" (`POST /api/setup/complete` signals `setupComplete` with no cameras).
3. `main.go` unblocks, calls `config.Load()` on the written file.
4. Normal initialization proceeds: seed auth users to DB, start detector, cameras, recorder, MQTT, etc.
5. `server.SetSubsystems()` wires everything into the running API server.
6. API switches from setup routes to full routes.

No process restart. The API server instance persists. Only its available routes change.

---

## Testing Strategy

### Unit Tests

- **config:** `LoadOrDefault` returns setup mode when file missing, error on invalid file, normal config on valid file. Zero cameras passes validation.
- **setup API:** `POST /api/setup` writes valid config, hashes password correctly, returns YAML when path is read-only.
- **config writing:** `POST /api/cameras` appends cameras via `yaml.Node` without corrupting existing content. Verify comments in other sections are preserved.
- **auth seeding:** Config users are seeded into DB on startup. Existing DB users are not overwritten.

### Integration Test

Start the binary with no config file. Verify:
1. Setup endpoints respond, all other routes redirect
2. `POST /api/setup` creates a valid config file with hashed password
3. Discovery endpoint returns results (may need mock or real ONVIF device)
4. `POST /api/cameras` appends cameras to config
5. System transitions to running mode — dashboard API returns camera data
6. Setup endpoints are no longer accessible

---

## Out of Scope (Future Work)

- Full config editor UI (editing recording settings, detection params, etc.)
- `vedetta init` CLI wizard (the web onboarding supersedes this roadmap item)
- Camera management UI for existing configs (edit/delete cameras post-setup)
- HTTPS/TLS setup during onboarding
- Multi-user management UI
