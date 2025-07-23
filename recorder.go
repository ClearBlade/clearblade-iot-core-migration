package main

import (
	"encoding/csv"
	"encoding/json"
	cbiotcore "github.com/clearblade/go-iot"
	"log"
	"os"
	"sync"
)

type csvRecorder struct {
	deviceConfigLock  *sync.Mutex
	deviceConfigsFile *os.File
	deviceConfigs     *csv.Writer
}

// TODO not just recorder, orchestrator - "migrator"

func newCSVRecorder() (*csvRecorder, error) {
	if err := os.MkdirAll(Args.workDir, os.ModePerm); err != nil {
		return nil, err
	}
	deviceConfigsFile, err := os.OpenFile(Args.workDir+"/deviceConfigs.csv", os.O_APPEND|os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return nil, err
	}
	deviceConfigs := csv.NewWriter(deviceConfigsFile)
	deviceConfigs.Comma = ';'
	_ = deviceConfigs.Write([]string{"deviceId", "config", "error"})
	return &csvRecorder{
		deviceConfigLock:  &sync.Mutex{},
		deviceConfigsFile: deviceConfigsFile,
		deviceConfigs:     deviceConfigs,
	}, nil
}

func (r *csvRecorder) close() {
	r.deviceConfigs.Flush()
	if err := r.deviceConfigs.Error(); err != nil {
		log.Printf("Error flushing csv recorder: %s\n", err)
	}
	_ = r.deviceConfigsFile.Close()
}

func (r *csvRecorder) RecordDeviceConfig(deviceId string, config []*cbiotcore.DeviceConfig) {
	log.Printf("Recording device config for device %s\n", deviceId)
	configStr, err := json.Marshal(config)
	if err != nil {
		log.Printf("Error marshalling config: %v\n", err)
		return
	}
	log.Printf("config: %+v\n", string(configStr))
	r.deviceConfigLock.Lock()
	defer r.deviceConfigLock.Unlock()
	err = r.deviceConfigs.Write([]string{
		deviceId,
		string(configStr),
		"", // error
	})
	if err != nil {
		log.Printf("Failed to record device config: %s\n", err)
	}
}

func (r *csvRecorder) RecordDeviceConfigError(deviceId string, configErr error) {
	log.Printf("Recording device error for device %s\n", deviceId)
	r.deviceConfigLock.Lock()
	defer r.deviceConfigLock.Unlock()
	err := r.deviceConfigs.Write([]string{
		deviceId,
		"", // config
		configErr.Error(),
	})
	if err != nil {
		log.Printf("Failed to record device config: %s\n", err)
	}
}
