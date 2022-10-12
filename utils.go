package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	gcpiotpb "google.golang.org/genproto/googleapis/cloud/iot/v1"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
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
	return strings.TrimSuffix(input, "\n"), nil
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

	return "https://community.clearblade.com"
	// return "https://" + region + ".clearblade.com"
}

func getAbsPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}

	if path[0] != '~' {
		return path, nil
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
		Id:      device.Name,
		Blocked: device.Blocked,
		Config: DeviceConfig{
			Version:         fmt.Sprint(device.Config.Version),
			CloudUpdateTime: getTimeString(device.Config.CloudUpdateTime.AsTime()),
			DeviceAckTime:   getTimeString(device.Config.DeviceAckTime.AsTime()),
			BinaryData:      string(device.Config.BinaryData),
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
			UpdateTime: device.State.UpdateTime.String(),
			BinaryData: string(device.State.BinaryData),
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
