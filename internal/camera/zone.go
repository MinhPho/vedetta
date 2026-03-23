package camera

import (
	"encoding/json"
	"time"
)

// Zone represents a spatial region on a camera view.
// Coordinates are percentages (0.0-1.0) relative to the frame dimensions.
type Zone struct {
	ID              int      `json:"id"`
	Camera          string   `json:"camera"`
	Name            string   `json:"name"`
	X1              float64  `json:"x1"`
	Y1              float64  `json:"y1"`
	X2              float64  `json:"x2"`
	Y2              float64  `json:"y2"`
	Labels          []string `json:"labels"`
	TrackPresence   bool     `json:"track_presence"`
	FaceRecognition bool     `json:"face_recognition"`
	Enabled         bool     `json:"enabled"`
}

// ZonePresence tracks the presence state of a label within a zone.
type ZonePresence struct {
	ZoneID      int       `json:"zone_id"`
	Label       string    `json:"label"`
	Present     bool      `json:"present"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
	LastChanged time.Time `json:"last_changed,omitempty"`
}

// LabelsJSON returns the JSON representation of the zone's labels.
func (z Zone) LabelsJSON() string {
	data, _ := json.Marshal(z.Labels)
	return string(data)
}

// MatchZones returns the zones that overlap with a detection bounding box.
// box is in pixel coordinates [x1, y1, x2, y2], frameW/frameH for normalization.
// A detection matches a zone if:
//  1. The zone is enabled
//  2. The detection label is in the zone's label list (empty labels = match all)
//  3. The detection box overlaps the zone by > 50% of the detection's area
func MatchZones(zones []Zone, box [4]int, label string, frameW, frameH int) []Zone {
	if frameW <= 0 || frameH <= 0 {
		return nil
	}

	// Normalize detection box to percentages
	dx1 := float64(box[0]) / float64(frameW)
	dy1 := float64(box[1]) / float64(frameH)
	dx2 := float64(box[2]) / float64(frameW)
	dy2 := float64(box[3]) / float64(frameH)

	detArea := (dx2 - dx1) * (dy2 - dy1)
	if detArea <= 0 {
		return nil
	}

	var matched []Zone
	for _, z := range zones {
		if !z.Enabled {
			continue
		}

		if !zoneMatchesLabel(z, label) {
			continue
		}

		// Compute intersection
		ix1 := max(dx1, z.X1)
		iy1 := max(dy1, z.Y1)
		ix2 := min(dx2, z.X2)
		iy2 := min(dy2, z.Y2)

		if ix1 >= ix2 || iy1 >= iy2 {
			continue
		}

		interArea := (ix2 - ix1) * (iy2 - iy1)
		if interArea/detArea > 0.5 {
			matched = append(matched, z)
		}
	}

	return matched
}

func zoneMatchesLabel(z Zone, label string) bool {
	if len(z.Labels) == 0 {
		return true
	}
	for _, l := range z.Labels {
		if l == label {
			return true
		}
	}
	return false
}

