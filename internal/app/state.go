package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"weekly-report/internal/report"
)

type DeliveryRecord struct {
	Status      string           `json:"status"`
	MessageID   string           `json:"message_id,omitempty"`
	Error       string           `json:"error,omitempty"`
	AttemptedAt string           `json:"attempted_at,omitempty"`
	History     []DeliveryRecord `json:"history,omitempty"`
}

type DeliveryState struct {
	mu      sync.Mutex
	path    string
	lock    *os.File
	records map[string]DeliveryRecord
}

func openDeliveryState(appDir string) (*DeliveryState, error) {
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(appDir, "state.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("another weekly-report run is active: %w", err)
	}
	state := &DeliveryState{path: filepath.Join(appDir, "state.json"), lock: lock, records: make(map[string]DeliveryRecord)}
	data, err := os.ReadFile(state.path)
	if err != nil && !os.IsNotExist(err) {
		state.Close()
		return nil, fmt.Errorf("read delivery state: %w", err)
	}
	if len(data) != 0 {
		if err := json.Unmarshal(data, &state.records); err != nil {
			state.Close()
			return nil, fmt.Errorf("decode delivery state: %w", err)
		}
		if state.records == nil {
			state.records = make(map[string]DeliveryRecord)
		}
	}
	return state, nil
}

func (state *DeliveryState) Close() error {
	if state.lock == nil {
		return nil
	}
	err := state.lock.Close()
	state.lock = nil
	return err
}

func deliveryKey(window report.ReportWindow, listID, address string) string {
	return window.Start.Format("2006-01-02") + "\x00" + window.End.Format("2006-01-02") + "\x00" + listID + "\x00" + strings.ToLower(address)
}

func (state *DeliveryState) get(key string) (DeliveryRecord, bool) {
	state.mu.Lock()
	defer state.mu.Unlock()
	record, ok := state.records[key]
	return record, ok
}

func (state *DeliveryState) set(key string, record DeliveryRecord) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if old, ok := state.records[key]; ok {
		record.History = old.History
		record.AttemptedAt = old.AttemptedAt
	}
	return state.replaceLocked(key, record)
}

func (state *DeliveryState) beginAttempt(key string, force bool) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	old, exists := state.records[key]
	if exists && !force {
		return fmt.Errorf("recipient was already attempted for this window")
	}
	record := DeliveryRecord{Status: "uncertain", AttemptedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if exists {
		record.History = append([]DeliveryRecord(nil), old.History...)
		old.History = nil
		record.History = append(record.History, old)
	}
	return state.replaceLocked(key, record)
}

func (state *DeliveryState) replaceLocked(key string, record DeliveryRecord) error {
	old, existed := state.records[key]
	state.records[key] = record
	if err := state.save(); err != nil {
		if existed {
			state.records[key] = old
		} else {
			delete(state.records, key)
		}
		return err
	}
	return nil
}

func (state *DeliveryState) save() error {
	data, err := json.MarshalIndent(state.records, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(state.path), ".state-*")
	if err != nil {
		return fmt.Errorf("create temporary delivery state: %w", err)
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), state.path); err != nil {
		return fmt.Errorf("replace delivery state: %w", err)
	}
	dir, err := os.Open(filepath.Dir(state.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
