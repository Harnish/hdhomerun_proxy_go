package main

import (
	"fmt"
	"sync"
	"time"
)

// TunerState represents the state of a single tuner
type TunerState struct {
	Index          int
	Channel        string // Current channel
	Program        string // Current program/virtual channel
	SessionID      string // Unique session ID for current lock
	Status         string // "idle", "tuning", "locked"
	Tuning         bool
	VCT            bool // Virtual channel table received
	SignalStrength int
	BitRate        int
	TargetIP       string    // Client IP address
	TargetPort     int       // Client port
	LockedAt       time.Time // When the tuner was locked
}

// TunerStateManager manages state for multiple tuners
type TunerStateManager struct {
	mu     sync.RWMutex
	tuners map[int]*TunerState
	count  int
}

// NewTunerStateManager creates a new state manager for the given number of tuners
func NewTunerStateManager(tunerCount int) *TunerStateManager {
	tm := &TunerStateManager{
		tuners: make(map[int]*TunerState),
		count:  tunerCount,
	}
	for i := 0; i < tunerCount; i++ {
		tm.tuners[i] = &TunerState{
			Index:          i,
			Status:         "idle",
			Channel:        "",
			Program:        "",
			SessionID:      "00000000",
			Tuning:         false,
			VCT:            false,
			SignalStrength: 0,
			BitRate:        0,
			TargetIP:       "0.0.0.0",
			TargetPort:     0,
		}
	}
	return tm
}

// GetTuner returns a copy of the tuner state at index
func (tm *TunerStateManager) GetTuner(index int) (*TunerState, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return nil, fmt.Errorf("tuner %d not found", index)
	}

	// Return a copy to prevent external modification
	copy := *tuner
	return &copy, nil
}

// SetTunerChannel sets the channel on a tuner
func (tm *TunerStateManager) SetTunerChannel(index int, channel string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	tuner.Channel = channel
	tuner.Status = "locked"
	tuner.Tuning = true
	tuner.LockedAt = time.Now()

	return nil
}

// ReleaseTuner releases a tuner back to idle state
func (tm *TunerStateManager) ReleaseTuner(index int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	tuner.Channel = ""
	tuner.Program = ""
	tuner.Status = "idle"
	tuner.Tuning = false
	tuner.SessionID = "00000000"
	tuner.TargetIP = "0.0.0.0"
	tuner.TargetPort = 0

	return nil
}

// LockTuner locks a tuner for streaming
func (tm *TunerStateManager) LockTuner(index int, sessionID, targetIP string, targetPort int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	if tuner.Status != "idle" {
		return fmt.Errorf("tuner %d is already in use (status: %s)", index, tuner.Status)
	}

	tuner.SessionID = sessionID
	tuner.TargetIP = targetIP
	tuner.TargetPort = targetPort
	tuner.Status = "locked"
	tuner.LockedAt = time.Now()

	return nil
}

// UnlockTuner unlocks a tuner
func (tm *TunerStateManager) UnlockTuner(index int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	tuner.Status = "idle"
	tuner.SessionID = "00000000"
	tuner.TargetIP = "0.0.0.0"
	tuner.TargetPort = 0
	tuner.Tuning = false

	return nil
}

// GetAllTuners returns a snapshot of all tuner states
func (tm *TunerStateManager) GetAllTuners() []*TunerState {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	states := make([]*TunerState, 0, len(tm.tuners))
	for i := 0; i < tm.count; i++ {
		if tuner, ok := tm.tuners[i]; ok {
			copy := *tuner
			states = append(states, &copy)
		}
	}
	return states
}

// SetSignalStrength sets the signal strength for a tuner
func (tm *TunerStateManager) SetSignalStrength(index int, strength int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	tuner.SignalStrength = strength
	return nil
}

// SetBitRate sets the bit rate for a tuner
func (tm *TunerStateManager) SetBitRate(index int, bitrate int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tuner, ok := tm.tuners[index]
	if !ok {
		return fmt.Errorf("tuner %d not found", index)
	}

	tuner.BitRate = bitrate
	return nil
}

// GetTunerCount returns the number of tuners
func (tm *TunerStateManager) GetTunerCount() int {
	return tm.count
}
