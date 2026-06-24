package main

import (
	"bufio"
	"bytes"
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
	StartTime         time.Time              `json:"start_time"`
	LastUpdated       time.Time              `json:"last_updated"`
	CurrentPhase      MigrationPhase         `json:"current_phase"`
	CompletedPhases   []MigrationPhase       `json:"completed_phases"`
	DevicesFetched    map[string]struct{}    `json:"devices_fetched"`
	DevicesMigrated   map[string]struct{}    `json:"devices_migrated"`
	ConfigsProcessed  map[string]struct{}    `json:"configs_processed"`
	ConfigHistory     map[string]interface{} `json:"config_history"`
	GatewaysProcessed map[string]struct{}    `json:"gateways_processed"`
	TotalDevices      int                    `json:"total_devices"`
	Args              DeviceMigratorArgs     `json:"args"`
	mutex             sync.RWMutex           `json:"-"`
	dirty             bool                   `json:"-"`
	saveTimer         *time.Timer            `json:"-"`
	deviceFile        *os.File               `json:"-"`
	deviceFileMu      sync.Mutex             `json:"-"`
}

var globalCheckpoint *CheckpointState

func getCheckpointFilePath() string {
	return filepath.Join(Args.workDir, "migration_checkpoint.json")
}

func getDevicesFilePath() string {
	return filepath.Join(Args.workDir, "devices.json")
}

func NewCheckpointState() *CheckpointState {
	c := &CheckpointState{
		StartTime:         time.Now(),
		LastUpdated:       time.Now(),
		CurrentPhase:      PhaseDeviceFetch,
		CompletedPhases:   []MigrationPhase{},
		DevicesFetched:    make(map[string]struct{}),
		DevicesMigrated:   make(map[string]struct{}),
		ConfigsProcessed:  make(map[string]struct{}),
		ConfigHistory:     make(map[string]interface{}),
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
		if c.dirty {
			if err := c.Save(); err != nil {
				printfColored(colorYellow, "Warning: Failed to auto-save checkpoint: %v", err)
			}
		}
		c.mutex.Unlock()
		// Schedule next tick after releasing the mutex to avoid holding it
		// while time.AfterFunc allocates its goroutine.
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

func (c *CheckpointState) AddFetchedDevice(deviceId string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.DevicesFetched[deviceId] = struct{}{}
	c.markDirty()
}

func (c *CheckpointState) AddMigratedDevice(deviceId string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.DevicesMigrated[deviceId] = struct{}{}
	c.markDirty()
}

func (c *CheckpointState) AddProcessedConfig(deviceId string, deviceConfig map[string]interface{}) {
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

func (c *CheckpointState) GetConfigHistory() map[string]interface{} {
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

// AppendDeviceToFile writes a single device to the devices file in JSON lines format.
// Safe to call concurrently from multiple goroutines.
func (c *CheckpointState) AppendDeviceToFile(device *cbiotcore.Device) error {
	c.deviceFileMu.Lock()
	defer c.deviceFileMu.Unlock()

	if c.deviceFile == nil {
		if err := os.MkdirAll(Args.workDir, 0755); err != nil {
			return fmt.Errorf("failed to create work directory: %w", err)
		}
		f, err := os.OpenFile(getDevicesFilePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open devices file: %w", err)
		}
		c.deviceFile = f
	}

	data, err := json.Marshal(device)
	if err != nil {
		return fmt.Errorf("failed to marshal device: %w", err)
	}
	data = append(data, '\n')
	_, err = c.deviceFile.Write(data)
	return err
}

// SaveAllDevicesToFile writes all devices to the devices file at once, replacing any
// existing content. Use this after a bulk fetch (fetchAllDevices) rather than
// AppendDeviceToFile to avoid the per-device open overhead.
func (c *CheckpointState) SaveAllDevicesToFile(devices []*cbiotcore.Device) error {
	c.deviceFileMu.Lock()
	defer c.deviceFileMu.Unlock()

	// Close any open append handle before truncating.
	if c.deviceFile != nil {
		c.deviceFile.Close()
		c.deviceFile = nil
	}

	if err := os.MkdirAll(Args.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	f, err := os.Create(getDevicesFilePath())
	if err != nil {
		return fmt.Errorf("failed to create devices file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 4*1024*1024)
	for _, device := range devices {
		data, err := json.Marshal(device)
		if err != nil {
			return fmt.Errorf("failed to marshal device: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

// CloseDeviceFile flushes and closes the open device file handle (if any).
func (c *CheckpointState) CloseDeviceFile() {
	c.deviceFileMu.Lock()
	defer c.deviceFileMu.Unlock()
	if c.deviceFile != nil {
		_ = c.deviceFile.Sync()
		c.deviceFile.Close()
		c.deviceFile = nil
	}
}

// LoadDevicesFromFile reads all devices from the devices file (JSON lines format).
// Returns nil, nil if the file does not exist.
func LoadDevicesFromFile() ([]*cbiotcore.Device, error) {
	path := getDevicesFilePath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open devices file: %w", err)
	}
	defer f.Close()

	var devices []*cbiotcore.Device
	scanner := bufio.NewScanner(f)
	// Allow up to 10 MB per line to accommodate devices with large payloads.
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var device cbiotcore.Device
		if err := json.Unmarshal(line, &device); err != nil {
			return nil, fmt.Errorf("failed to parse device from file: %w", err)
		}
		devices = append(devices, &device)
	}
	return devices, scanner.Err()
}

func (c *CheckpointState) Complete() error {
	// Close the device file before acquiring the checkpoint mutex to avoid
	// lock ordering issues.
	c.CloseDeviceFile()

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

	devicesPath := getDevicesFilePath()
	if err := os.Remove(devicesPath); err != nil && !os.IsNotExist(err) {
		printfColored(colorYellow, "Warning: Could not remove devices file: %v", err)
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
