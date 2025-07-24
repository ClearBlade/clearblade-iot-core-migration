package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	cbiotcore "github.com/clearblade/go-iot"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	colorCyan   = "\033[36m"
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

func init() {
	if runtime.GOOS == "windows" {
		colorCyan = ""
		colorReset = ""
		colorGreen = ""
		colorYellow = ""
		colorRed = ""
	}
}

func printfColored(color, format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}
	if format[len(format)-1] != '\n' {
		format += "\n"
	}
	fmt.Printf(color+format+colorReset, args...)
}

func readCsvFile(filePath string) ([][]string, error) {
	printfColored(colorGreen, "\u2713 Reading CSV file at %s", filePath)
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	return records, nil
}

func parseDeviceIds(rows [][]string) []string {
	printfColored(colorGreen, "\u2713 Parsing device IDs")
	var deviceIDs []string

	if len(rows) == 0 {
		log.Fatal("empty CSV file")
	}

	header := rows[0]
	idx := -1
	for i, name := range header {
		if name == "deviceId" {
			idx = i
			break
		}
	}
	if idx == -1 {
		log.Fatal("deviceId column not found")
	}

	for _, row := range rows[1:] {
		if len(row) > idx {
			deviceIDs = append(deviceIDs, row[idx])
		}
	}

	return deviceIDs
}

func getCBProjectID(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalln("Error when opening json file: ", err)
	}

	var payload CBConfig
	err = json.Unmarshal(content, &payload)
	if err != nil {
		log.Fatalln("Error during Unmarshal(): ", err)
	}

	return payload.Project
}

func getCBSourceDevicePath(deviceId string) string {
	return fmt.Sprintf("%s/devices/%s", getCBSourceRegistryPath(), deviceId)
}

func getCBSourceRegistryPath() string {
	val, _ := getAbsPath(Args.cbSourceServiceAccount)
	parent := fmt.Sprintf("projects/%s/locations/%s/registries/%s", getCBProjectID(val), Args.cbSourceRegion, Args.cbSourceRegistryName)
	return parent
}

func getCBRegistryPath() string {
	val, _ := getAbsPath(Args.cbServiceAccount)
	parent := fmt.Sprintf("projects/%s/locations/%s/registries/%s", getCBProjectID(val), Args.cbRegistryRegion, Args.cbRegistryName)
	return parent
}

func getCBDevicePath(deviceId string) string {
	return fmt.Sprintf("%s/devices/%s", getCBRegistryPath(), deviceId)
}

func readInput(msg string) (string, error) {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	// remove the delimiter from the string
	input = strings.TrimSuffix(input, "\n")
	input = strings.TrimSuffix(input, "\r")

	return input, nil
}

type progressBar struct {
	*progressbar.ProgressBar
}

func (pb *progressBar) Add(num int) {
	if err := pb.ProgressBar.Add(num); err != nil {
		log.Printf("Unable to add %d to progress bar: %s\n", num, err)
	}
}

func (pb *progressBar) Finish() {
	if err := pb.ProgressBar.Finish(); err != nil {
		log.Printf("Unable to finish progress bar: %s\n", err)
	}
}

func getProgressBar(total int, description string) *progressBar {
	description = colorYellow + description + colorReset
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	return &progressBar{bar}
}

func getSpinner(description string) *progressBar {
	description = colorYellow + description + colorReset
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
	)
	return &progressBar{bar}
}

type PaginatedRequest interface {
	Do() (*cbiotcore.ListDevicesResponse, error)
	PageToken(token string) *cbiotcore.ProjectsLocationsRegistriesDevicesListCall
}

func paginatedFetch(req PaginatedRequest, spinnerDesc string) ([]*cbiotcore.Device, error) {
	var spinner *progressBar
	if spinnerDesc != "" {
		spinner = getSpinner(spinnerDesc)
		defer spinner.Finish()
	}

	resp, err := req.Do()
	if err != nil {
		return nil, err
	}

	var allDevices []*cbiotcore.Device
	allDevices = append(allDevices, resp.Devices...)

	for resp.NextPageToken != "" {
		if spinner != nil {
			spinner.Add(1)
		}
		resp, err = req.PageToken(resp.NextPageToken).Do()
		if err != nil {
			return nil, err
		}
		allDevices = append(allDevices, resp.Devices...)
	}

	return allDevices, nil
}

func getAbsPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}

	if path[0] != '~' {
		return strings.TrimSuffix(path, "\r"), nil
	}

	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return "", errors.New("cannot expand user-specific home dir")
	}

	usr, _ := user.Current()
	dir := usr.HomeDir

	return filepath.Join(dir, path[1:]), nil
}

func transform(device *cbiotcore.Device) *cbiotcore.Device {
	parsedCreds := make([]*cbiotcore.DeviceCredential, 0)
	if Args.updatePublicKeys {
		for _, creds := range device.Credentials {
			parsedCreds = append(parsedCreds, &cbiotcore.DeviceCredential{
				ExpirationTime: creds.ExpirationTime,
				PublicKey: &cbiotcore.PublicKeyCredential{
					Format: creds.PublicKey.Format,
					Key:    creds.PublicKey.Key,
				},
			})
		}
	}

	cbDevice := &cbiotcore.Device{
		Id:          device.Id,
		Blocked:     device.Blocked,
		Credentials: parsedCreds,
		LogLevel:    device.LogLevel,
		Metadata:    device.Metadata,
		Name:        device.Id,
		NumId:       device.NumId,
	}

	if device.Config != nil && !Args.skipConfig {
		cbDevice.Config = &cbiotcore.DeviceConfig{
			Version:         device.Config.Version,
			CloudUpdateTime: device.Config.CloudUpdateTime,
			DeviceAckTime:   device.Config.DeviceAckTime,
			BinaryData:      device.Config.BinaryData,
		}
	}

	if device.GatewayConfig != nil {
		cbDevice.GatewayConfig = &cbiotcore.GatewayConfig{
			GatewayType:             device.GatewayConfig.GatewayType,
			GatewayAuthMethod:       device.GatewayConfig.GatewayAuthMethod,
			LastAccessedGatewayId:   device.GatewayConfig.LastAccessedGatewayId,
			LastAccessedGatewayTime: device.GatewayConfig.LastAccessedGatewayTime,
		}
	}

	return cbDevice
}

func ExportDeviceBatches(devices []*cbiotcore.Device, batchSize int64) {
	batches := make(map[int][]*cbiotcore.Device)

	for i, device := range devices {
		batchNumber := i / int(batchSize)
		batches[batchNumber] = append(batches[batchNumber], device)
	}

	for i, batch := range batches {
		WriteBatchFile(batch, fmt.Sprintf("batch_%d.csv", i))
	}
}

func WriteBatchFile(devices []*cbiotcore.Device, filename string) {
	currDir, err := os.Getwd()
	if err != nil {
		log.Fatalln("Could not get current wd: ", err)
	}

	failedDevicesFile := fmt.Sprint(currDir, "/", filename)

	if runtime.GOOS == "windows" {
		failedDevicesFile = fmt.Sprint(currDir, "\\", filename)
	}

	f, err := os.OpenFile(failedDevicesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalln("Could not open file: ", err)
	}
	defer f.Close()

	fileContents := "deviceId\n"
	for _, device := range devices {
		fileContents += device.Id
		fileContents += "\n"
	}

	if _, err := f.WriteString(fileContents); err != nil {
		log.Fatalln("Could not write to file: ", err)
	}
}
