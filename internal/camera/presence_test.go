package camera

import (
	"testing"
	"time"
)

func newTestTracker() *PresenceTracker {
	pt := NewPresenceTracker()
	pt.debounceEnter = 3 * time.Second
	pt.debounceLeave = 30 * time.Second
	return pt
}

var testZoneNames = map[int]string{1: "driveway", 2: "doorbell"}

func TestPresence_EnterDebounce(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// First detection: start enter timer, no event yet
	events := pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events on first detection, got %d", len(events))
	}

	// 2 seconds later: still below debounce threshold
	now = now.Add(2 * time.Second)
	events = pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events before debounce, got %d", len(events))
	}

	// 1 more second (total 3s): debounce met, should enter
	now = now.Add(1 * time.Second)
	events = pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 1 {
		t.Fatalf("expected 1 enter event, got %d", len(events))
	}
	if events[0].Type != "zone_enter" {
		t.Errorf("expected zone_enter, got %q", events[0].Type)
	}
	if events[0].Label != "car" {
		t.Errorf("expected label 'car', got %q", events[0].Label)
	}
	if events[0].ZoneName != "driveway" {
		t.Errorf("expected zone 'driveway', got %q", events[0].ZoneName)
	}
}

func TestPresence_LeaveDebounce(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Enter the zone (skip debounce by advancing past it)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	now = now.Add(4 * time.Second)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	// Now stop detecting
	now = now.Add(1 * time.Second)
	events := pt.Update(map[PresenceKey]bool{}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events on first miss, got %d", len(events))
	}

	// 29 seconds later: still below leave debounce
	now = now.Add(29 * time.Second)
	events = pt.Update(map[PresenceKey]bool{}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events before leave debounce, got %d", len(events))
	}

	// 1 more second (total 30s): should leave
	now = now.Add(1 * time.Second)
	events = pt.Update(map[PresenceKey]bool{}, testZoneNames)
	if len(events) != 1 {
		t.Fatalf("expected 1 leave event, got %d", len(events))
	}
	if events[0].Type != "zone_leave" {
		t.Errorf("expected zone_leave, got %q", events[0].Type)
	}
}

func TestPresence_CancelEnterOnGap(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Start detecting
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	// 2 seconds later
	now = now.Add(2 * time.Second)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	// Gap: stop detecting for a frame
	now = now.Add(500 * time.Millisecond)
	pt.Update(map[PresenceKey]bool{}, testZoneNames)

	// Resume detecting: enter timer should restart
	now = now.Add(500 * time.Millisecond)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	// 2 seconds later: should NOT enter yet (timer was reset)
	now = now.Add(2 * time.Second)
	events := pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events (timer reset after gap), got %d", len(events))
	}

	// 1 more second (3s total since re-detection): should enter now
	now = now.Add(1 * time.Second)
	events = pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 1 {
		t.Fatalf("expected 1 enter event, got %d", len(events))
	}
}

func TestPresence_CancelLeaveOnDetection(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Enter zone
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	now = now.Add(4 * time.Second)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	// Stop detecting for 20 seconds (below leave threshold)
	now = now.Add(20 * time.Second)
	pt.Update(map[PresenceKey]bool{}, testZoneNames)

	// Resume detecting: leave timer should be cancelled
	now = now.Add(1 * time.Second)
	events := pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events when re-detecting, got %d", len(events))
	}

	// Verify still present
	present, _, _ := pt.GetPresence(key)
	if !present {
		t.Error("expected still present after re-detection")
	}

	// Stop detecting again. The leave timer restarts fresh.
	now = now.Add(1 * time.Second)
	pt.Update(map[PresenceKey]bool{}, testZoneNames)

	// 29 seconds later: should not have left yet
	now = now.Add(29 * time.Second)
	events = pt.Update(map[PresenceKey]bool{}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events before leave debounce, got %d", len(events))
	}

	// 1 more second (30s total since second stop): should leave
	now = now.Add(1 * time.Second)
	events = pt.Update(map[PresenceKey]bool{}, testZoneNames)
	if len(events) != 1 {
		t.Fatalf("expected 1 leave event, got %d", len(events))
	}
}

func TestPresence_GetPresence(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Not tracked yet
	present, _, _ := pt.GetPresence(key)
	if present {
		t.Error("expected not present before any detection")
	}

	// Enter zone
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	now = now.Add(4 * time.Second)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	present, lastSeen, lastChanged := pt.GetPresence(key)
	if !present {
		t.Error("expected present after entering")
	}
	if lastSeen.IsZero() {
		t.Error("expected lastSeen to be set")
	}
	if lastChanged.IsZero() {
		t.Error("expected lastChanged to be set")
	}
}

func TestPresence_MultipleLabelsSameZone(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	carKey := PresenceKey{ZoneID: 1, Label: "car"}
	truckKey := PresenceKey{ZoneID: 1, Label: "truck"}

	// Both car and truck in zone
	pt.Update(map[PresenceKey]bool{carKey: true, truckKey: true}, testZoneNames)
	now = now.Add(4 * time.Second)
	events := pt.Update(map[PresenceKey]bool{carKey: true, truckKey: true}, testZoneNames)

	if len(events) != 2 {
		t.Fatalf("expected 2 enter events, got %d", len(events))
	}

	// Remove truck only - first frame without truck starts leave timer
	now = now.Add(1 * time.Second)
	events = pt.Update(map[PresenceKey]bool{carKey: true}, testZoneNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events before leave debounce, got %d", len(events))
	}

	// 30 seconds later: truck leave debounce met
	now = now.Add(30 * time.Second)
	events = pt.Update(map[PresenceKey]bool{carKey: true}, testZoneNames)

	// Truck should leave, car should stay
	if len(events) != 1 {
		t.Fatalf("expected 1 leave event, got %d", len(events))
	}
	if events[0].Label != "truck" {
		t.Errorf("expected truck to leave, got %q", events[0].Label)
	}

	// Car should still be present
	present, _, _ := pt.GetPresence(carKey)
	if !present {
		t.Error("expected car still present")
	}
}

func TestPresence_AllPresence(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Enter
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
	now = now.Add(4 * time.Second)
	pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)

	all := pt.AllPresence()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
	if !all[key].Present {
		t.Error("expected present=true")
	}
}

func TestPresence_RapidTransitions(t *testing.T) {
	pt := newTestTracker()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	pt.now = func() time.Time { return now }

	key := PresenceKey{ZoneID: 1, Label: "car"}

	// Rapid on-off-on-off should NOT trigger enter
	for i := 0; i < 10; i++ {
		now = now.Add(1 * time.Second)
		events := pt.Update(map[PresenceKey]bool{key: true}, testZoneNames)
		if len(events) != 0 {
			t.Fatalf("unexpected event during rapid transitions at iteration %d", i)
		}
		now = now.Add(500 * time.Millisecond)
		pt.Update(map[PresenceKey]bool{}, testZoneNames)
	}

	// Should still not be present
	present, _, _ := pt.GetPresence(key)
	if present {
		t.Error("expected not present during rapid transitions")
	}
}
