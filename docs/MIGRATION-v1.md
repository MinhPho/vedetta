# Vedetta v1 Migration Guide

This release hardens Vedetta for internet-facing deployments and intentionally breaks the old config and auth surface.

## Config Changes

- Replace `auth.username` / `auth.password` with:

```yaml
auth:
  users:
    - username: admin
      password_hash: "<bcrypt hash>"
```

- Replace `detect.motion_threshold` with:

```yaml
detect:
  motion:
    pixel_threshold: 25
    min_area: 200
    background_alpha: 0.05
    min_region_score: 0.02
```

- Replace rectangular zone config:

```yaml
zones:
  - name: driveway
    coordinates: [x1, y1, x2, y2]
    objects: [person, car]
```

with polygon points:

```yaml
zones:
  - name: driveway
    points:
      - [0.10, 0.50]
      - [0.90, 0.50]
      - [0.90, 1.00]
      - [0.10, 1.00]
    labels: [person, car]
```

- `cameras[].enabled` now preserves explicit `false`.
- `cameras[].detect.enabled` is new and defaults to `true`.
- `events.retain_days` is new and defaults to `90`.
- `api.exposure` and `api.trusted_proxies[]` are new. Use `api.exposure=internet` only with TLS or a trusted HTTPS-terminating proxy.

## Auth Changes

- HTTP Basic Auth is removed from the web/API surface.
- Browser access now uses session cookies plus CSRF protection.
- API integrations should create scoped bearer tokens through `POST /api/tokens`.
- Generate bcrypt hashes with:

```sh
vedetta auth hash-password '<password>'
```

## Runtime Changes

- Event rows are canonical metadata. Missing clips or snapshots no longer delete the event row.
- Latest camera snapshots now live at `<recording.path>/<camera>/latest.jpg`.
- Vedetta no longer downloads the ONNX model or OpenH264 at runtime. Install or bundle them before deployment.
