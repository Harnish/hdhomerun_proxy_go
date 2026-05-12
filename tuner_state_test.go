package main

import (
	"testing"
	"time"
)

func TestNewTunerStateManager(t *testing.T) {
	tm := NewTunerStateManager(4)
	if tm.GetTunerCount() != 4 {
		t.Errorf("Expected 4 tuners, got %d", tm.GetTunerCount())
	}

	for i := 0; i < 4; i++ {
		tuner, err := tm.GetTuner(i)
		if err != nil {
			t.Errorf("GetTuner(%d) error: %v", i, err)
		}
		if tuner.Index != i {
			t.Errorf("Tuner %d has wrong index: %d", i, tuner.Index)
		}
		if tuner.Status != "idle" {
			t.Errorf("Tuner %d should be idle, got %s", i, tuner.Status)
		}
	}
}

func TestGetTunerNotFound(t *testing.T) {
	tm := NewTunerStateManager(2)
	_, err := tm.GetTuner(5)
	if err == nil {
		t.Errorf("Expected error for non-existent tuner")
	}
}

func TestSetTunerChannel(t *testing.T) {
	tm := NewTunerStateManager(4)
	channel := "2.1"

	err := tm.SetTunerChannel(0, channel)
	if err != nil {
		t.Errorf("SetTunerChannel error: %v", err)
	}

	tuner, _ := tm.GetTuner(0)
	if tuner.Channel != channel {
		t.Errorf("Expected channel %q, got %q", channel, tuner.Channel)
	}
	if tuner.Status != "locked" {
		t.Errorf("Expected status 'locked', got %q", tuner.Status)
	}
	if !tuner.Tuning {
		t.Errorf("Expected tuner.Tuning to be true")
	}
}

func TestReleaseTuner(t *testing.T) {
	tm := NewTunerStateManager(4)

	// First lock a tuner
	tm.SetTunerChannel(0, "2.1")
	tuner, _ := tm.GetTuner(0)
	if tuner.Status != "locked" {
		t.Fatalf("Tuner should be locked")
	}

	// Then release it
	err := tm.ReleaseTuner(0)
	if err != nil {
		t.Errorf("ReleaseTuner error: %v", err)
	}

	tuner, _ = tm.GetTuner(0)
	if tuner.Status != "idle" {
		t.Errorf("Expected status 'idle', got %q", tuner.Status)
	}
	if tuner.Channel != "" {
		t.Errorf("Expected empty channel, got %q", tuner.Channel)
	}
	if tuner.Tuning {
		t.Errorf("Expected tuner.Tuning to be false")
	}
}

func TestLockTuner(t *testing.T) {
	tm := NewTunerStateManager(4)

	sessionID := "ABC123DE"
	targetIP := "192.168.1.100"
	targetPort := 12345

	err := tm.LockTuner(0, sessionID, targetIP, targetPort)
	if err != nil {
		t.Errorf("LockTuner error: %v", err)
	}

	tuner, _ := tm.GetTuner(0)
	if tuner.SessionID != sessionID {
		t.Errorf("Expected sessionID %q, got %q", sessionID, tuner.SessionID)
	}
	if tuner.TargetIP != targetIP {
		t.Errorf("Expected targetIP %q, got %q", targetIP, tuner.TargetIP)
	}
	if tuner.TargetPort != targetPort {
		t.Errorf("Expected targetPort %d, got %d", targetPort, tuner.TargetPort)
	}
}

func TestLockAlreadyLockedTuner(t *testing.T) {
	tm := NewTunerStateManager(4)

	// Lock first time
	tm.LockTuner(0, "AAAA", "192.168.1.1", 1000)

	// Try to lock again - should fail
	err := tm.LockTuner(0, "BBBB", "192.168.1.2", 2000)
	if err == nil {
		t.Errorf("Expected error when locking already-locked tuner")
	}
}

func TestUnlockTuner(t *testing.T) {
	tm := NewTunerStateManager(4)

	// Lock then unlock
	tm.LockTuner(0, "ABC123DE", "192.168.1.100", 12345)
	err := tm.UnlockTuner(0)
	if err != nil {
		t.Errorf("UnlockTuner error: %v", err)
	}

	tuner, _ := tm.GetTuner(0)
	if tuner.Status != "idle" {
		t.Errorf("Expected idle status, got %q", tuner.Status)
	}
	if tuner.SessionID != "00000000" {
		t.Errorf("Expected default sessionID, got %q", tuner.SessionID)
	}
}

func TestGetAllTuners(t *testing.T) {
	tm := NewTunerStateManager(3)

	// Modify some tuners
	tm.SetTunerChannel(0, "2.1")
	tm.SetTunerChannel(2, "5.1")

	all := tm.GetAllTuners()
	if len(all) != 3 {
		t.Errorf("Expected 3 tuners, got %d", len(all))
	}

	if all[0].Channel != "2.1" {
		t.Errorf("Tuner 0 should have channel 2.1, got %q", all[0].Channel)
	}
	if all[1].Channel != "" {
		t.Errorf("Tuner 1 should have empty channel, got %q", all[1].Channel)
	}
	if all[2].Channel != "5.1" {
		t.Errorf("Tuner 2 should have channel 5.1, got %q", all[2].Channel)
	}
}

func TestSetSignalStrength(t *testing.T) {
	tm := NewTunerStateManager(2)

	err := tm.SetSignalStrength(0, 85)
	if err != nil {
		t.Errorf("SetSignalStrength error: %v", err)
	}

	tuner, _ := tm.GetTuner(0)
	if tuner.SignalStrength != 85 {
		t.Errorf("Expected signal strength 85, got %d", tuner.SignalStrength)
	}
}

func TestSetBitRate(t *testing.T) {
	tm := NewTunerStateManager(2)

	err := tm.SetBitRate(1, 19200000)
	if err != nil {
		t.Errorf("SetBitRate error: %v", err)
	}

	tuner, _ := tm.GetTuner(1)
	if tuner.BitRate != 19200000 {
		t.Errorf("Expected bitrate 19200000, got %d", tuner.BitRate)
	}
}

func TestTunerGetIsACopy(t *testing.T) {
	tm := NewTunerStateManager(1)

	tuner1, _ := tm.GetTuner(0)
	tuner1.Channel = "modified"

	// Get the same tuner again
	tuner2, _ := tm.GetTuner(0)
	if tuner2.Channel != "" {
		t.Errorf("Getting tuner twice should return copies; modification should not persist")
	}
}

func TestSetTunerChannelUpdatesTime(t *testing.T) {
	tm := NewTunerStateManager(1)

	before := time.Now()
	tm.SetTunerChannel(0, "2.1")
	after := time.Now()

	tuner, _ := tm.GetTuner(0)
	if tuner.LockedAt.Before(before) || tuner.LockedAt.After(after) {
		t.Errorf("LockedAt time not set correctly")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tm := NewTunerStateManager(4)

	// Simulate concurrent access
	done := make(chan bool, 10)

	// Multiple goroutines setting channels
	for i := 0; i < 4; i++ {
		go func(idx int) {
			tm.SetTunerChannel(idx, "test")
			done <- true
		}(i)
	}

	// Multiple goroutines getting tuners
	for i := 0; i < 6; i++ {
		go func(idx int) {
			tm.GetTuner(idx % 4)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state
	all := tm.GetAllTuners()
	for _, tuner := range all {
		if tuner.Channel != "test" {
			t.Errorf("Tuner %d has channel %q, expected 'test'", tuner.Index, tuner.Channel)
		}
	}
}
