package camera

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/rvben/watchpost/internal/config"
	"github.com/rvben/watchpost/internal/detect"
)

// Event represents a detected object event from a camera.
type Event struct {
	ID         string    `json:"id"`
	CameraName string    `json:"camera"`
	Label      string    `json:"label"`
	Score      float32   `json:"score"`
	Box        [4]int    `json:"box"` // x1, y1, x2, y2
	Timestamp  time.Time `json:"timestamp"`
	SnapshotPath string  `json:"snapshot_path,omitempty"`
	ClipPath   string    `json:"clip_path,omitempty"`
}

// Camera manages a single RTSP camera stream.
type Camera struct {
	config         config.CameraConfig
	detector       *detect.Detector
	tracker        *detect.Tracker
	motionDetector *detect.MotionDetector
	events         chan<- Event
	hwaccel        *HWAccel

	mu              sync.RWMutex
	lastSnapshot    *image.RGBA
	lastMotion      time.Time
	confirmedTracks map[int]bool
}

func NewCamera(cfg config.CameraConfig, detector *detect.Detector, events chan<- Event, hwaccel *HWAccel) *Camera {
	return &Camera{
		config:          cfg,
		detector:        detector,
		tracker:         detect.NewTracker(30, 3),
		motionDetector:  detect.NewMotionDetector(25, 200, 0.05),
		events:          events,
		hwaccel:         hwaccel,
		confirmedTracks: make(map[int]bool),
	}
}

func (c *Camera) Name() string {
	return c.config.Name
}

func (c *Camera) RecordURL() string {
	if c.config.RecordURL != "" {
		return c.config.RecordURL
	}
	return c.config.URL
}

func (c *Camera) LastSnapshot() *image.RGBA {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSnapshot
}

// Start begins reading frames from the RTSP stream.
func (c *Camera) Start(ctx context.Context) {
	slog.Info("starting camera", "name", c.config.Name, "url", c.config.URL)

	go c.readFrames(ctx)
}

// readFrames connects to the RTSP stream via ffmpeg and decodes frames.
func (c *Camera) readFrames(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := c.runFFmpeg(ctx); err != nil {
			slog.Error("ffmpeg stream error, reconnecting",
				"camera", c.config.Name,
				"error", err,
			)
			// Wait before reconnecting
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

// runFFmpeg spawns an ffmpeg process that decodes RTSP to raw frames on stdout.
func (c *Camera) runFFmpeg(ctx context.Context) error {
	w := c.config.Detect.Width
	h := c.config.Detect.Height
	fps := c.config.Detect.FPS

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
	}
	args = append(args, c.hwaccel.FFmpegArgs()...)
	args = append(args,
		"-i", c.config.URL,
		"-vf", fmt.Sprintf("fps=%d,scale=%d:%d", fps, w, h),
		"-pix_fmt", "rgb24",
		"-f", "rawvideo",
		"-",
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	frameSize := w * h * 3 // RGB24
	buf := make([]byte, frameSize)

	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		default:
		}

		n, err := readFull(stdout, buf)
		if err != nil || n != frameSize {
			_ = cmd.Process.Kill()
			return fmt.Errorf("read frame: %w (got %d bytes)", err, n)
		}

		// Convert to image
		img := rawToRGBA(buf, w, h)

		c.mu.Lock()
		c.lastSnapshot = img
		c.mu.Unlock()

		// Contour-based motion detection
		motionRegions := c.motionDetector.Detect(buf, w, h)
		if len(motionRegions) > 0 {
			c.mu.Lock()
			c.lastMotion = time.Now()
			c.mu.Unlock()

			// Run object detection on the full frame when motion is detected.
			// The motion regions tell us WHERE motion is, but the YOLO model
			// expects a full frame (it handles its own letterboxing/scaling).
			detections := c.detector.Detect(img)
			tracked := c.tracker.Update(detections)

			// Emit events for newly confirmed tracks
			for _, obj := range tracked {
				if !c.confirmedTracks[obj.TrackID] {
					c.confirmedTracks[obj.TrackID] = true
					c.events <- Event{
						ID:         fmt.Sprintf("%s-t%d-%d", c.config.Name, obj.TrackID, time.Now().UnixMilli()),
						CameraName: c.config.Name,
						Label:      obj.Label,
						Score:      obj.Score,
						Box:        obj.Box,
						Timestamp:  time.Now(),
					}
				}
			}

			// Emit end events for deleted tracks
			for _, obj := range c.tracker.DeletedTracks() {
				delete(c.confirmedTracks, obj.TrackID)
				c.events <- Event{
					ID:         fmt.Sprintf("%s-t%d-end-%d", c.config.Name, obj.TrackID, time.Now().UnixMilli()),
					CameraName: c.config.Name,
					Label:      obj.Label,
					Score:      obj.Score,
					Box:        obj.Box,
					Timestamp:  time.Now(),
				}
			}
		}
	}
}

func rawToRGBA(data []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcIdx := (y*w + x) * 3
			dstIdx := (y*w + x) * 4
			img.Pix[dstIdx+0] = data[srcIdx+0] // R
			img.Pix[dstIdx+1] = data[srcIdx+1] // G
			img.Pix[dstIdx+2] = data[srcIdx+2] // B
			img.Pix[dstIdx+3] = 255             // A
		}
	}
	return img
}

func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
