package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	cbiotcore "github.com/clearblade/go-iot"
)

type MigrationPhase string

const (
	PhaseDeviceFetch    MigrationPhase = "device_fetch"
	PhaseDeviceMigrate  MigrationPhase = "device_migrate"
	PhaseConfigHistory  MigrationPhase = "config_history"
	PhaseGatewayBinding MigrationPhase = "gateway_binding"
	PhaseComplete       MigrationPhase = "complete"
)

type CheckpointState struct {
	StartTime         time.Time                            `json:"start_time"`
	LastUpdated       time.Time                            `json:"last_updated"`
	CurrentPhase      MigrationPhase                       `json:"current_phase"`
	CompletedPhases   []MigrationPhase                     `json:"completed_phases"`
	DevicesFetched    map[string]*cbiotcore.Device         `json:"devices_fetched"`
	DevicesMigrated   map[string]struct{}                  `json:"devices_migrated"`
	ConfigsProcessed  map[string]struct{}                  `json:"configs_processed"`
	ConfigHistory     map[string][]*cbiotcore.DeviceConfig `json:"config_history"`
	GatewaysProcessed map[string]struct{}                  `json:"gateways_processed"`
	TotalDevices      int                                  `json:"total_devices"`
	Args              DeviceMigratorArgs                   `json:"args"`
	mutex             sync.RWMutex                         `json:"-"`
	dirty             bool                                 `json:"-"`
	saveTimer         *time.Timer                          `json:"-"`
}

var globalCheckpoint *CheckpointState

func getCheckpointFilePath() string {
	return filepath.Join(Args.workDir, "migration_checkpoint.json")
}

func NewCheckpointState() *CheckpointState {
	c := &CheckpointState{
		StartTime:         time.Now(),
		LastUpdated:       time.Now(),
		CurrentPhase:      PhaseDeviceFetch,
		CompletedPhases:   []MigrationPhase{},
		DevicesFetched:    make(map[string]*cbiotcore.Device),
		DevicesMigrated:   make(map[string]struct{}),
		ConfigsProcessed:  make(map[string]struct{}),
		ConfigHistory:     make(map[string][]*cbiotcore.DeviceConfig),
		GatewaysProcessed: make(map[string]struct{}),
		Args:              Args,
		dirty:             false,
	}
	c.startSaveTimer()
	return c
}

func LoadCheckpoint() (*CheckpointState, error) {
	checkpointPath := getCheckpointFilePath()

	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	var state CheckpointState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse checkpoint file: %w", err)
	}

	state.dirty = false
	state.startSaveTimer()
	return &state, nil
}

func (c *CheckpointState) Save() error {
	c.LastUpdated = time.Now()

	if err := os.MkdirAll(Args.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint state: %w", err)
	}

	checkpointPath := getCheckpointFilePath()
	if err := os.WriteFile(checkpointPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint file: %w", err)
	}

	c.dirty = false
	return nil
}

func (c *CheckpointState) markDirty() {
	c.dirty = true
}

func (c *CheckpointState) startSaveTimer() {
	if c.saveTimer != nil {
		c.saveTimer.Stop()
	}
	c.saveTimer = time.AfterFunc(5*time.Second, func() {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		if c.dirty {
			if err := c.Save(); err != nil {
				printfColored(colorYellow, "Warning: Failed to auto-save checkpoint: %v", err)
			}
		}
		c.startSaveTimer()
	})
}

func (c *CheckpointState) FlushToDisk() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.dirty {
		return c.Save()
	}
	return nil
}

func (c *CheckpointState) SetPhase(phase MigrationPhase) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.CurrentPhase != phase {
		c.CompletedPhases = append(c.CompletedPhases, c.CurrentPhase)
	}
	c.CurrentPhase = phase
	if err := c.Save(); err != nil {
		log.Fatalf("failed to save checkpoint state: %s\n", err)
	}
}

func (c *CheckpointState) AddFetchedDevice(device *cbiotcore.Device) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.DevicesFetched[device.Id] = device
	c.markDirty()
}

func (c *CheckpointState) AddMigratedDevice(deviceId string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.DevicesMigrated[deviceId] = struct{}{}
	c.markDirty()
}

func (c *CheckpointState) AddProcessedConfig(deviceId string, deviceConfig []*cbiotcore.DeviceConfig) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.ConfigsProcessed[deviceId] = struct{}{}
	c.ConfigHistory[deviceId] = deviceConfig
	c.markDirty()
}

func (c *CheckpointState) AddProcessedGateway(gatewayId string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.GatewaysProcessed[gatewayId] = struct{}{}
	c.markDirty()
}

func (c *CheckpointState) SetTotalDevices(count int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.TotalDevices = count
	c.markDirty()
}

func (c *CheckpointState) IsPhaseCompleted(phase MigrationPhase) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	for _, completed := range c.CompletedPhases {
		if completed == phase {
			return true
		}
	}
	return false
}

func (c *CheckpointState) GetUnfetchedDeviceIds(deviceIds []string) []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var unfetchedDeviceIds []string
	for _, deviceId := range deviceIds {
		if _, ok := c.DevicesFetched[deviceId]; !ok {
			unfetchedDeviceIds = append(unfetchedDeviceIds, deviceId)
		}
	}

	return unfetchedDeviceIds
}

func (c *CheckpointState) GetFetchedDevices() []*cbiotcore.Device {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	devices := make([]*cbiotcore.Device, 0, len(c.DevicesFetched))
	for _, device := range c.DevicesFetched {
		devices = append(devices, device)
	}
	return devices
}

func (c *CheckpointState) GetConfigHistory() map[string][]*cbiotcore.DeviceConfig {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.ConfigHistory // TODO
}

func (c *CheckpointState) GetUnprocessedGateways(gatewayBindings map[string][]*cbiotcore.Device) []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	var unprocessedGateways []string
	for gateway := range gatewayBindings {
		if _, ok := c.GatewaysProcessed[gateway]; !ok {
			unprocessedGateways = append(unprocessedGateways, gateway)
		}
	}
	return unprocessedGateways
}

func (c *CheckpointState) GetRemainingDevicesForMigration(allDevices []*cbiotcore.Device) []*cbiotcore.Device {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var remaining []*cbiotcore.Device
	for _, device := range allDevices {
		if _, ok := c.DevicesMigrated[device.Id]; !ok {
			remaining = append(remaining, device)
		}
	}
	return remaining
}

func (c *CheckpointState) GetRemainingDevicesForConfig(allDevices []*cbiotcore.Device) []*cbiotcore.Device {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var remaining []*cbiotcore.Device
	for _, device := range allDevices {
		if _, ok := c.ConfigsProcessed[device.Id]; !ok {
			remaining = append(remaining, device)
		}
	}
	return remaining
}

func (c *CheckpointState) Complete() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.CurrentPhase = PhaseComplete
	c.CompletedPhases = append(c.CompletedPhases, PhaseComplete)

	if err := c.Save(); err != nil {
		return err
	}

	checkpointPath := getCheckpointFilePath()
	if err := os.Remove(checkpointPath); err != nil {
		printfColored(colorYellow, "Warning: Could not remove checkpoint file: %v", err)
	}

	return nil
}

func InitializeCheckpointSystem() error {
	var err error

	globalCheckpoint, err = LoadCheckpoint()
	if err != nil {
		return fmt.Errorf("failed to load checkpoint: %w", err)
	}

	if globalCheckpoint != nil {
		printfColored(colorCyan, "Found existing checkpoint - resuming migration from phase: %s", globalCheckpoint.CurrentPhase)
		printfColored(colorCyan, "Progress: %d devices fetched, %d migrated, %d configs processed",
			len(globalCheckpoint.DevicesFetched),
			len(globalCheckpoint.DevicesMigrated),
			len(globalCheckpoint.ConfigsProcessed))
	} else {
		printfColored(colorCyan, "Starting fresh migration with checkpoint tracking")
		globalCheckpoint = NewCheckpointState()
		if err := globalCheckpoint.Save(); err != nil {
			return fmt.Errorf("failed to save initial checkpoint: %w", err)
		}
	}

	return nil
}

func GetCheckpoint() *CheckpointState {
	return globalCheckpoint
}
