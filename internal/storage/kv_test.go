package storage

import (
	"testing"
)

func TestKVStore_GetSetDelete(t *testing.T) {
	db := newTestDB(t)

	// Get non-existent key returns empty string and no error
	val, err := db.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("GetSetting error: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty string, got %q", val)
	}

	// Set and get
	if err := db.SetSetting("dismissed_update", "v0.3.0"); err != nil {
		t.Fatalf("SetSetting error: %v", err)
	}
	val, err = db.GetSetting("dismissed_update")
	if err != nil {
		t.Fatalf("GetSetting error: %v", err)
	}
	if val != "v0.3.0" {
		t.Fatalf("expected v0.3.0, got %q", val)
	}

	// Overwrite
	if err := db.SetSetting("dismissed_update", "v0.4.0"); err != nil {
		t.Fatalf("SetSetting overwrite error: %v", err)
	}
	val, _ = db.GetSetting("dismissed_update")
	if val != "v0.4.0" {
		t.Fatalf("expected v0.4.0, got %q", val)
	}

	// Delete
	if err := db.DeleteSetting("dismissed_update"); err != nil {
		t.Fatalf("DeleteSetting error: %v", err)
	}
	val, _ = db.GetSetting("dismissed_update")
	if val != "" {
		t.Fatalf("expected empty after delete, got %q", val)
	}

	// Delete non-existent key is not an error
	if err := db.DeleteSetting("nonexistent"); err != nil {
		t.Fatalf("DeleteSetting non-existent error: %v", err)
	}
}
