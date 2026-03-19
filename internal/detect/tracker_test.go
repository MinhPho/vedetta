package detect

import "testing"

func TestIoU_PerfectOverlap(t *testing.T) {
	a := [4]int{0, 0, 100, 100}
	v := iou(a, a)
	if v != 1.0 {
		t.Errorf("expected 1.0, got %f", v)
	}
}

func TestIoU_NoOverlap(t *testing.T) {
	a := [4]int{0, 0, 50, 50}
	b := [4]int{60, 60, 100, 100}
	v := iou(a, b)
	if v != 0 {
		t.Errorf("expected 0, got %f", v)
	}
}

func TestIoU_PartialOverlap(t *testing.T) {
	a := [4]int{0, 0, 100, 100}
	b := [4]int{50, 50, 150, 150}
	v := iou(a, b)
	// Intersection: 50*50 = 2500
	// Union: 10000 + 10000 - 2500 = 17500
	expected := 2500.0 / 17500.0
	if v < expected-0.001 || v > expected+0.001 {
		t.Errorf("expected ~%f, got %f", expected, v)
	}
}

func TestTracker_SingleObjectMoving(t *testing.T) {
	tr := NewTracker(3, 2)

	// Frame 1: object appears
	objs := tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
	})
	// minHits=2, so not yet confirmed
	if len(objs) != 0 {
		t.Errorf("frame 1: expected 0 confirmed, got %d", len(objs))
	}

	// Frame 2: same object, slightly moved (high IoU)
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.92, Box: [4]int{12, 12, 62, 62}},
	})
	if len(objs) != 1 {
		t.Fatalf("frame 2: expected 1 confirmed, got %d", len(objs))
	}
	if objs[0].TrackID != 1 {
		t.Errorf("expected TrackID 1, got %d", objs[0].TrackID)
	}
	if objs[0].Label != "person" {
		t.Errorf("expected label person, got %s", objs[0].Label)
	}

	// Frame 3: object moves further
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.88, Box: [4]int{15, 15, 65, 65}},
	})
	if len(objs) != 1 {
		t.Fatalf("frame 3: expected 1 confirmed, got %d", len(objs))
	}
	if objs[0].TrackID != 1 {
		t.Errorf("expected same TrackID 1, got %d", objs[0].TrackID)
	}
}

func TestTracker_MultipleObjects(t *testing.T) {
	tr := NewTracker(3, 1) // minHits=1 for immediate confirmation

	objs := tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
		{Label: "car", Score: 0.85, Box: [4]int{200, 200, 400, 400}},
	})
	if len(objs) != 2 {
		t.Fatalf("expected 2 confirmed, got %d", len(objs))
	}

	ids := map[int]string{}
	for _, o := range objs {
		ids[o.TrackID] = o.Label
	}

	// Both should have unique IDs
	if len(ids) != 2 {
		t.Errorf("expected 2 unique IDs, got %d", len(ids))
	}

	// Frame 2: both move slightly
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.91, Box: [4]int{12, 12, 62, 62}},
		{Label: "car", Score: 0.86, Box: [4]int{202, 202, 402, 402}},
	})
	if len(objs) != 2 {
		t.Fatalf("frame 2: expected 2 confirmed, got %d", len(objs))
	}

	// IDs should be preserved
	for _, o := range objs {
		if o.Label == "person" && o.TrackID != 1 {
			t.Errorf("person should keep TrackID 1, got %d", o.TrackID)
		}
		if o.Label == "car" && o.TrackID != 2 {
			t.Errorf("car should keep TrackID 2, got %d", o.TrackID)
		}
	}
}

func TestTracker_ObjectDisappearsAndReappears(t *testing.T) {
	tr := NewTracker(2, 1) // maxDisappeared=2, confirm after 1 hit

	// Frame 1: object appears
	objs := tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
	})
	if len(objs) != 1 {
		t.Fatalf("expected 1, got %d", len(objs))
	}
	originalID := objs[0].TrackID

	// Frame 2: no detections
	objs = tr.Update(nil)
	// Track still alive (disappeared=1, maxDisappeared=2)
	if len(objs) != 1 {
		t.Fatalf("frame 2: expected 1 (still alive), got %d", len(objs))
	}

	// Frame 3: still no detections
	objs = tr.Update(nil)
	// disappeared=2, still alive (deleted when > maxDisappeared)
	if len(objs) != 1 {
		t.Fatalf("frame 3: expected 1 (still alive at boundary), got %d", len(objs))
	}

	// Frame 4: still no detections -> disappeared=3 > maxDisappeared=2, deleted
	objs = tr.Update(nil)
	if len(objs) != 0 {
		t.Fatalf("frame 4: expected 0 (deleted), got %d", len(objs))
	}

	// Frame 5: object reappears in same location -> gets new ID
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
	})
	if len(objs) != 1 {
		t.Fatalf("frame 5: expected 1, got %d", len(objs))
	}
	if objs[0].TrackID == originalID {
		t.Errorf("reappeared object should get new ID, but got same ID %d", originalID)
	}
}

func TestTracker_OverlappingObjectsDifferentClasses(t *testing.T) {
	tr := NewTracker(3, 1)

	// Two overlapping objects with different labels
	objs := tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 100, 100}},
		{Label: "dog", Score: 0.8, Box: [4]int{30, 30, 120, 120}},
	})
	if len(objs) != 2 {
		t.Fatalf("expected 2, got %d", len(objs))
	}

	// Frame 2: both move
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.91, Box: [4]int{12, 12, 102, 102}},
		{Label: "dog", Score: 0.82, Box: [4]int{32, 32, 122, 122}},
	})
	if len(objs) != 2 {
		t.Fatalf("frame 2: expected 2, got %d", len(objs))
	}

	labels := map[string]bool{}
	for _, o := range objs {
		labels[o.Label] = true
	}
	if !labels["person"] || !labels["dog"] {
		t.Errorf("expected both person and dog, got %v", labels)
	}
}

func TestTracker_NoDetectionsFrame(t *testing.T) {
	tr := NewTracker(5, 1)

	// Create two tracks
	tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
		{Label: "car", Score: 0.85, Box: [4]int{200, 200, 400, 400}},
	})

	// Empty frame
	objs := tr.Update(nil)
	// Both should still be confirmed (disappeared=1, maxDisappeared=5)
	if len(objs) != 2 {
		t.Errorf("expected 2 tracks still alive, got %d", len(objs))
	}
}

func TestTracker_MinHitsConfirmation(t *testing.T) {
	tr := NewTracker(3, 3) // Requires 3 consecutive hits

	// Frame 1
	objs := tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
	})
	if len(objs) != 0 {
		t.Errorf("frame 1: should not be confirmed yet, got %d", len(objs))
	}

	// Frame 2
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{12, 12, 62, 62}},
	})
	if len(objs) != 0 {
		t.Errorf("frame 2: should not be confirmed yet, got %d", len(objs))
	}

	// Frame 3: now hits=3, should be confirmed
	objs = tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{14, 14, 64, 64}},
	})
	if len(objs) != 1 {
		t.Errorf("frame 3: should be confirmed, got %d", len(objs))
	}
}

func TestTracker_DeletedTracksReturned(t *testing.T) {
	tr := NewTracker(0, 1) // maxDisappeared=0 -> deleted immediately on miss

	tr.Update([]Detection{
		{Label: "person", Score: 0.9, Box: [4]int{10, 10, 60, 60}},
	})

	// Object disappears
	tr.Update(nil)

	deleted := tr.DeletedTracks()
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted track, got %d", len(deleted))
	}
	if deleted[0].Label != "person" {
		t.Errorf("expected label person, got %s", deleted[0].Label)
	}
}

func TestTracker_EmptyUpdate(t *testing.T) {
	tr := NewTracker(3, 1)
	objs := tr.Update(nil)
	if len(objs) != 0 {
		t.Errorf("expected 0 from empty tracker, got %d", len(objs))
	}
}
