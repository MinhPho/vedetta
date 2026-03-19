package detect

import "sort"

// TrackState represents the lifecycle state of a tracked object.
type TrackState string

const (
	TrackTentative TrackState = "tentative"
	TrackConfirmed TrackState = "confirmed"
	TrackDeleted   TrackState = "deleted"
)

// TrackedObject is the external representation of a confirmed track.
type TrackedObject struct {
	TrackID    int
	Label      string
	Score      float32
	Box        [4]int // x1, y1, x2, y2
	State      string
	FramesSeen int
}

// track is the internal state for a single tracked object.
type track struct {
	id           int
	label        string
	box          [4]int
	score        float32
	age          int
	hits         int
	disappeared  int
	state        TrackState
}

// Tracker matches detections across frames using IoU to maintain stable object identities.
type Tracker struct {
	maxDisappeared int
	minHits        int
	nextID         int
	tracks         []*track
}

// NewTracker creates a tracker with the given parameters.
// maxDisappeared: frames before a track is deleted.
// minHits: consecutive frames before a tentative track is confirmed.
func NewTracker(maxDisappeared, minHits int) *Tracker {
	return &Tracker{
		maxDisappeared: maxDisappeared,
		minHits:        minHits,
		nextID:         1,
	}
}

// Update processes a new set of detections and returns confirmed tracked objects.
// It also returns tracks that just transitioned to confirmed or deleted state
// so the caller can emit start/end events.
func (t *Tracker) Update(detections []Detection) []TrackedObject {
	// Remove previously deleted tracks
	alive := t.tracks[:0]
	for _, tr := range t.tracks {
		if tr.state != TrackDeleted {
			alive = append(alive, tr)
		}
	}
	t.tracks = alive

	// Age all tracks
	for _, tr := range t.tracks {
		tr.age++
	}

	// If no existing tracks, create new ones from all detections
	if len(t.tracks) == 0 {
		for _, d := range detections {
			t.tracks = append(t.tracks, t.newTrack(d))
		}
		return t.confirmedObjects()
	}

	// If no detections, increment disappeared for all tracks
	if len(detections) == 0 {
		for _, tr := range t.tracks {
			tr.disappeared++
			if tr.disappeared > t.maxDisappeared {
				tr.state = TrackDeleted
			}
		}
		return t.confirmedObjects()
	}

	// Build IoU cost matrix and perform greedy assignment
	type assignment struct {
		trackIdx     int
		detectionIdx int
		iou          float64
	}

	var pairs []assignment
	for ti, tr := range t.tracks {
		for di, d := range detections {
			v := iou(tr.box, d.Box)
			if v > 0 {
				pairs = append(pairs, assignment{ti, di, v})
			}
		}
	}

	// Sort by IoU descending for greedy matching
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].iou > pairs[j].iou
	})

	matchedTracks := make(map[int]bool)
	matchedDets := make(map[int]bool)

	for _, p := range pairs {
		if matchedTracks[p.trackIdx] || matchedDets[p.detectionIdx] {
			continue
		}
		matchedTracks[p.trackIdx] = true
		matchedDets[p.detectionIdx] = true

		tr := t.tracks[p.trackIdx]
		d := detections[p.detectionIdx]
		tr.box = d.Box
		tr.score = d.Score
		tr.label = d.Label
		tr.hits++
		tr.disappeared = 0
		if tr.state == TrackTentative && tr.hits >= t.minHits {
			tr.state = TrackConfirmed
		}
	}

	// Unmatched tracks: increment disappeared
	for ti, tr := range t.tracks {
		if !matchedTracks[ti] {
			tr.disappeared++
			if tr.disappeared > t.maxDisappeared {
				tr.state = TrackDeleted
			}
		}
	}

	// Unmatched detections: create new tentative tracks
	for di, d := range detections {
		if !matchedDets[di] {
			t.tracks = append(t.tracks, t.newTrack(d))
		}
	}

	return t.confirmedObjects()
}

// DeletedTracks returns tracks that were just marked deleted in the last Update call.
func (t *Tracker) DeletedTracks() []TrackedObject {
	var result []TrackedObject
	for _, tr := range t.tracks {
		if tr.state == TrackDeleted {
			result = append(result, TrackedObject{
				TrackID:    tr.id,
				Label:      tr.label,
				Score:      tr.score,
				Box:        tr.box,
				State:      string(TrackDeleted),
				FramesSeen: tr.hits,
			})
		}
	}
	return result
}

func (t *Tracker) newTrack(d Detection) *track {
	tr := &track{
		id:    t.nextID,
		label: d.Label,
		box:   d.Box,
		score: d.Score,
		age:   1,
		hits:  1,
		state: TrackTentative,
	}
	t.nextID++
	if tr.hits >= t.minHits {
		tr.state = TrackConfirmed
	}
	return tr
}

func (t *Tracker) confirmedObjects() []TrackedObject {
	var result []TrackedObject
	for _, tr := range t.tracks {
		if tr.state == TrackConfirmed {
			result = append(result, TrackedObject{
				TrackID:    tr.id,
				Label:      tr.label,
				Score:      tr.score,
				Box:        tr.box,
				State:      string(tr.state),
				FramesSeen: tr.hits,
			})
		}
	}
	return result
}

