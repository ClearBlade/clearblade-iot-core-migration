package main

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	gcpiotpb "cloud.google.com/go/iot/apiv1/iotpb"
	cbiotcore "github.com/clearblade/go-iot"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

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
	content, err := os.ReadFile(filePath)
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

func getRegistryPath(region, registryId string) string {
	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := fmt.Sprintf("projects/%s/locations/%s/registries/%s", getProjectID(val), region, registryId)
	return parent
}

func getDevicePath(region, registryId, deviceId string) string {
	return fmt.Sprintf("%s/devices/%s", getRegistryPath(region, registryId), deviceId)
}

func getCbRegistryPath() string {
	return getRegistryPath(Args.cbRegistryRegion, Args.cbRegistryName)
}

func getGoogleRegistryPath() string {
	return getRegistryPath(Args.gcpRegistryRegion, Args.registryName)
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

func transform(device *gcpiotpb.Device) *cbiotcore.Device {

	parsedCreds := make([]*cbiotcore.DeviceCredential, 0)
	if Args.updatePublicKeys {
		for _, creds := range device.Credentials {
			parsedCreds = append(parsedCreds, &cbiotcore.DeviceCredential{
				ExpirationTime: getTimeString(creds.GetExpirationTime().AsTime()),
				PublicKey: &cbiotcore.PublicKeyCredential{
					Format: creds.GetPublicKey().Format.String(),
					Key:    creds.GetPublicKey().Key,
				},
			})
		}
	}

	cbDevice := &cbiotcore.Device{
		Id:          device.Id,
		Blocked:     device.Blocked,
		Credentials: parsedCreds,
		LogLevel:    device.LogLevel.String(),
		Metadata:    device.Metadata,
		Name:        device.Id,
		NumId:       device.NumId,
	}

	if device.Config != nil && !Args.skipConfig {
		cbDevice.Config = &cbiotcore.DeviceConfig{
			Version:         device.Config.Version,
			CloudUpdateTime: getTimeString(device.Config.CloudUpdateTime.AsTime()),
			DeviceAckTime:   getTimeString(device.Config.DeviceAckTime.AsTime()),
			BinaryData:      base64.StdEncoding.EncodeToString(device.Config.BinaryData),
		}
	}

	if device.GatewayConfig != nil {
		cbDevice.GatewayConfig = &cbiotcore.GatewayConfig{
			GatewayType:             device.GatewayConfig.GatewayType.String(),
			GatewayAuthMethod:       device.GatewayConfig.GatewayAuthMethod.String(),
			LastAccessedGatewayId:   device.GatewayConfig.LastAccessedGatewayId,
			LastAccessedGatewayTime: getTimeString(device.GatewayConfig.LastAccessedGatewayTime.AsTime()),
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

func generateFailedDevicesCSV(errorLogs []ErrorLog) error {
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

	fileContents := "context,error,deviceId\n"
	for i := 0; i < len(errorLogs); i++ {
		errMsg := ""
		if errorLogs[i].Error != nil {
			errMsg = errorLogs[i].Error.Error()
		}
		fileContents += fmt.Sprintf(`%s,"%s",%s\n`, errorLogs[i].Context, errMsg, errorLogs[i].DeviceId)
	}

	if _, err := f.WriteString(fileContents); err != nil {
		return err
	}

	return nil
}
