package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

type GCPConfig struct {
	Project_id string `json:"project_id"`
}

type CBConfig struct {
	Project string `json:"project"`
}

type ErrorLog struct {
	Context  string
	Error    error
	DeviceId string
}

type ErrorLogger struct {
	logs []ErrorLog
	lock *sync.Mutex
}

func NewErrorLogger() *ErrorLogger {
	return &ErrorLogger{
		logs: make([]ErrorLog, 0),
		lock: &sync.Mutex{},
	}
}

func (el *ErrorLogger) AddError(context, deviceId string, e error) {
	el.AddErrorLog(ErrorLog{
		Context:  context,
		DeviceId: deviceId,
		Error:    e,
	})
}

func (el *ErrorLogger) AddErrorLog(log ErrorLog) {
	el.lock.Lock()
	defer el.lock.Unlock()
	el.logs = append(el.logs, log)
}

func (el *ErrorLogger) WriteToFile() {
	el.lock.Lock()
	defer el.lock.Unlock()

	if len(el.logs) == 0 {
		return
	}

	currDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current directory: %v", err)
	}

	failedDevicesFile := fmt.Sprint(currDir, "/failed_devices_", time.Now().Format("2006-01-02T15:04:05"), ".csv")
	if runtime.GOOS == "windows" {
		failedDevicesFile = fmt.Sprint(currDir, "\\failed_devices_", time.Now().Format("2006-01-02T15-04-05"), ".csv")
	}

	f, err := os.OpenFile(failedDevicesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open error log file %s: %v", failedDevicesFile, err)
	}
	defer f.Close()

	csvWriter := csv.NewWriter(f)
	err = csvWriter.Write([]string{"context", "error", "deviceId"})
	if err != nil {
		log.Fatalf("Failed to write to file %s: %v", failedDevicesFile, err)
	}

	for _, l := range el.logs {
		errMsg := ""
		if l.Error != nil {
			errMsg = l.Error.Error()
		}
		record := []string{l.Context, errMsg, l.DeviceId}
		err = csvWriter.Write(record)
		if err != nil {
			log.Printf("Failed to write record %s to file %s: %v", record, failedDevicesFile, err)
		}
	}

	csvWriter.Flush()
}

type counter struct {
	count int
	lock  *sync.Mutex
}

func newCounter() *counter {
	return &counter{
		count: 0,
		lock:  &sync.Mutex{},
	}
}

func (c *counter) Increment() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.count++
}

func (c *counter) Count() int {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.count
}

func (c *counter) SetCount(count int) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.count = count
}
