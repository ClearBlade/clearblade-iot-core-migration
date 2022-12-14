package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/exp/maps"
	gcpiotpb "google.golang.org/genproto/googleapis/cloud/iot/v1"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var regions = map[string]string{
	"us-central1":  "us-central1",
	"asia-east1":   "asia-east1",
	"europe-west1": "europe-west1",
}

func fileExists(filename string) bool {
	if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
		fmt.Println("File path does not exists: ", filename, "Error: ", err)
		return false
	}

	return true
}

func readCsvFile(filePath string) [][]string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatalln("Unable to read input file: ", filePath, err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		log.Fatalln("Unable to parse file as CSV for: ", filePath, err)
	}

	return records
}

func getProjectID(filePath string) string {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalln("Error when opening json file: ", err)
	}

	var payload Data
	err = json.Unmarshal(content, &payload)
	if err != nil {
		log.Fatalln("Error during Unmarshal(): ", err)
	}

	return payload.Project_id
}

func readInput(msg string) (string, error) {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	// remove the delimeter from the string
	input = strings.TrimSuffix(input, "\n")
	input = strings.TrimSuffix(input, "\r")

	return input, nil
}

func getProgressBar(total int, description string) *progressbar.ProgressBar {
	description = string(colorYellow) + description + string(colorReset)
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	return bar
}

func getSpinner(description string) *progressbar.ProgressBar {
	description = string(colorYellow) + description + string(colorReset)
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
	)
	return bar
}

func getURI(region string) string {
	if Args.sandbox {
		return "https://iot-sandbox.clearblade.com"
	}

	if !isValidRegion(region) {
		log.Fatalln("Provided region '", region, "' is not supported. Supported regions are: ", maps.Keys(regions))
	}

	return "https://" + region + ".clearblade.com"
}

func isValidRegion(region string) bool {
	if _, ok := regions[region]; !ok {
		return false
	}

	return true
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

func transform(device *gcpiotpb.Device) *CbDevice {

	parsedCreds := make([]CbDeviceCredential, 0)
	if Args.updatePublicKeys {
		for _, creds := range device.Credentials {
			parsedCreds = append(parsedCreds, CbDeviceCredential{
				ExpirationTime: getTimeString(creds.GetExpirationTime().AsTime()),
				PublicKey: IoTCorePublicKeyCredential{
					Format: creds.GetPublicKey().Format.String(),
					Key:    creds.GetPublicKey().Key,
				},
			})
		}
	}

	cbDevice := &CbDevice{
		Id:      device.Id,
		Blocked: device.Blocked,
		Config: DeviceConfig{
			Version:         fmt.Sprint(device.Config.Version),
			CloudUpdateTime: getTimeString(device.Config.CloudUpdateTime.AsTime()),
			DeviceAckTime:   getTimeString(device.Config.DeviceAckTime.AsTime()),
			BinaryData:      base64.StdEncoding.EncodeToString(device.Config.BinaryData),
		},
		GatewayConfig: GatewayConfig{
			GatewayType:             device.GatewayConfig.GatewayType.String(),
			GatewayAuthMethod:       device.GatewayConfig.GatewayAuthMethod.String(),
			LastAccessedGatewayId:   device.GatewayConfig.LastAccessedGatewayId,
			LastAccessedGatewayTime: getTimeString(device.GatewayConfig.LastAccessedGatewayTime.AsTime()),
		},
		Credentials:        parsedCreds,
		LastConfigAckTime:  getTimeString(device.LastConfigAckTime.AsTime()),
		LastConfigSendTime: getTimeString(device.LastConfigSendTime.AsTime()),
		LastErrorTime:      getTimeString(device.LastErrorTime.AsTime()),
		LastEventTime:      getTimeString(device.LastEventTime.AsTime()),
		LastHeartbeatTime:  getTimeString(device.LastHeartbeatTime.AsTime()),
		LastStateTime:      getTimeString(device.LastStateTime.AsTime()),
		LogLevel:           device.LogLevel.String(),
		Metadata:           device.Metadata,
		Name:               device.Id,
		NumId:              fmt.Sprint(device.NumId),
	}

	if device.State != nil {
		cbDevice.State = DeviceState{
			UpdateTime: getTimeString(device.State.UpdateTime.AsTime()),
			BinaryData: base64.StdEncoding.EncodeToString(device.State.BinaryData),
		}
	}

	if device.LastErrorStatus != nil {
		cbDevice.LastErrorStatus = DeviceLastErrorStatus{
			Code:    device.LastErrorStatus.Code,
			Message: device.LastErrorStatus.Message,
		}
	}

	return cbDevice
}

func getTimeString(timestamp time.Time) string {
	if timestamp.Unix() == 0 {
		return ""
	}
	return timestamp.Format(time.RFC3339)
}

func generateFailedDevicesCSV(fileContents string) error {
	currDir, err := os.Getwd()
	if err != nil {
		return err
	}

	failedDevicesFile := fmt.Sprint(currDir, "/failed_devices_", time.Now().Format("2006-01-02T15:04:05"), ".csv")

	if runtime.GOOS == "windows" {
		failedDevicesFile = fmt.Sprint(currDir, "\\failed_devices_", time.Now().Format("2006-01-02T15-04-05"), ".csv")
	}

	f, err := os.OpenFile(failedDevicesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer f.Close()

	if _, err := f.WriteString(fileContents); err != nil {
		return err
	}

	return nil
}
