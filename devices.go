package main

import (
	"bytes"
	gcpiotcore "cloud.google.com/go/iot/apiv1"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/api/iterator"
	gcpiotpb "google.golang.org/genproto/googleapis/cloud/iot/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"io/ioutil"
	"log"
	"math"
	"net/http"
)

var fields = &fieldmaskpb.FieldMask{
	Paths: []string{
		"id",
		"name",
		"credentials",
		"last_heartbeat_time",
		"last_event_time",
		"last_state_time",
		"last_config_ack_time",
		"last_config_send_time",
		"blocked",
		"last_error_time",
		"last_error_status",
		"config",
		"state",
		"log_level",
		"metadata",
		"gateway_config"},
}

func fetchDevicesFromGoogleIotCore(ctx context.Context, gcpClient *gcpiotcore.DeviceManagerClient) ([]*gcpiotpb.Device, map[string]interface{}) {

	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := "projects/" + getProjectID(val) + "/locations/" + Args.region + "/registries/" + Args.registryName

	if Args.devicesCsvFile != "" {
		return fetchDevicesFromCSV(ctx, gcpClient, parent)
	}

	return fetchAllDevices(ctx, gcpClient, parent)
}

func fetchDevicesFromCSV(ctx context.Context, client *gcpiotcore.DeviceManagerClient, parent string) ([]*gcpiotpb.Device, map[string]interface{}) {

	absDevicesCsvFilePath, err := getAbsPath(Args.devicesCsvFile)
	if err != nil {
		log.Fatalln("Cannot resolve devices CSV filepath: ", err.Error())
	}

	if !fileExists(absDevicesCsvFilePath) {
		log.Fatalln("Unable to locate device CSV filepath: ", absDevicesCsvFilePath)
	}

	records := readCsvFile(absDevicesCsvFilePath)
	var deviceIds []string
	for _, line := range records {
		deviceIds = append(deviceIds, line[0])
	}

	var devices []*gcpiotpb.Device
	deviceConfigs := make(map[string]interface{})

	if len(deviceIds) > 10000 {
		fmt.Println("\nMore than 10k devices specified in the CSV file. Preparing to batch fetch devices...")
		maxIterations := int(math.Floor(float64(len(deviceIds))/float64(10000))) + 1
		for i := 0; i < maxIterations; i++ {
			var batchDeviceIds []string
			if i == maxIterations-1 {
				batchDeviceIds = deviceIds[1+i*10000:]
			} else if i == 0 {
				batchDeviceIds = deviceIds[i*10000 : 10000+i*10000]
			} else {
				batchDeviceIds = deviceIds[1+i*10000 : 10000+i*10000]
			}

			req := &gcpiotpb.ListDevicesRequest{
				Parent:    parent,
				DeviceIds: batchDeviceIds,
				FieldMask: fields,
			}
			devicesSubset := fetchDevices(req, ctx, client, len(batchDeviceIds))
			devices = append(devices, devicesSubset...)
		}

		if Args.configHistory {
			for _, device := range devices {
				deviceConfigs[device.Id] = fetchConfigVersionHistory(device, ctx, client)
			}
		}

		defer client.Close()
		return devices, deviceConfigs
	}

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    parent,
		DeviceIds: deviceIds,
		FieldMask: fields,
	}

	devices = fetchDevices(req, ctx, client, len(deviceIds))

	if Args.configHistory {
		for _, device := range devices {
			deviceConfigs[device.Id] = fetchConfigVersionHistory(device, ctx, client)
		}
	}

	defer client.Close()
	return devices, deviceConfigs
}

func fetchAllDevices(ctx context.Context, client *gcpiotcore.DeviceManagerClient, parent string) ([]*gcpiotpb.Device, map[string]interface{}) {

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    parent,
		FieldMask: fields,
	}

	var devices []*gcpiotpb.Device
	it := client.ListDevices(ctx, req)
	fmt.Println()
	spinner := getSpinner("Fetching all devices from registry...")

	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}

		if err != nil {
			log.Fatalln("Unable to iterate over device records: ", err)
		}

		devices = append(devices, resp)
		if err := spinner.Add(1); err != nil {
			log.Fatalln("Unable to add to spinner: ", err)
		}
	}

	deviceConfigs := make(map[string]interface{})

	if Args.configHistory {
		fmt.Println()
		spinner := getSpinner("Fetching all device config history from registry...")
		for _, device := range devices {
			deviceConfigs[device.Id] = fetchConfigVersionHistory(device, ctx, client)
			if err := spinner.Add(1); err != nil {
				log.Fatalln("Unable to add to spinner: ", err)
			}
		}
	}

	defer client.Close()
	fmt.Println(string(colorGreen), "\u2713 Fetched", len(devices), "devices!", string(colorReset))
	return devices, deviceConfigs
}

func fetchDevices(req *gcpiotpb.ListDevicesRequest, ctx context.Context, client *gcpiotcore.DeviceManagerClient, devicesLength int) []*gcpiotpb.Device {
	var devices []*gcpiotpb.Device
	it := client.ListDevices(ctx, req)
	bar := getProgressBar(devicesLength, "Fetching devices from registry...")
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			if err := bar.Finish(); err != nil {
				log.Fatalln("Unable to finish progressbar: ", err)
			}

			if err := bar.Close(); err != nil {
				log.Fatalln("Unable to Close progressbar: ", err)
			}

			successMsg := "Fetched " + fmt.Sprint(len(devices)) + " devices!"
			fmt.Println(string(colorGreen), "\n\u2713", successMsg, string(colorReset))
			break
		}

		if err != nil {
			log.Fatalln("Unable to iterate over device records: ", err)
		}

		if err := bar.Add(1); err != nil {
			log.Fatalln("Unable to add to progressbar: ", err)
		}

		devices = append(devices, resp)
	}
	return devices
}

func fetchConfigVersionHistory(device *gcpiotpb.Device, ctx context.Context, client *gcpiotcore.DeviceManagerClient) map[string]interface{} {
	req := &gcpiotpb.ListDeviceConfigVersionsRequest{
		Name:        device.Name,
		NumVersions: 0,
	}

	deviceConfigVersions, err := client.ListDeviceConfigVersions(ctx, req)

	if err != nil {
		fmt.Println("Unable to fetch state history for device: ", device.Id, ". Reason: ", err)
	}

	configs := make(map[string]interface{})

	for _, config := range deviceConfigVersions.GetDeviceConfigs() {
		configs[fmt.Sprint(config.Version)] = map[string]interface{}{
			"cloudUpdateTime": getTimeString(config.CloudUpdateTime.AsTime()),
			"deviceAckTime":   getTimeString(config.DeviceAckTime.AsTime()),
			"binaryData":      string(config.BinaryData),
		}
	}

	return configs
}

func addDevicesToClearBlade(devices []*gcpiotpb.Device, deviceConfigs map[string]interface{}) {
	bar := getProgressBar(len(devices), "Migrating Devices...")
	i := 0
	for _, device := range devices {
		if barErr := bar.Add(1); barErr != nil {
			log.Fatalln("Unable to add to progressbar: ", barErr)
		}

		err := createDevice(device)
		if err != nil {
			err := updateDevice(device)
			if err != nil {
				log.Println("Unable to insert device: ", device.Id, ". Reason: ", err)
				continue
			}
		}
		i += 1
	}

	if deviceConfigs != nil {
		err := updateConfigHistory(deviceConfigs)
		if err != nil {
			fmt.Println("Unable to update config version history!")
		}
	}

	if i == len(devices) {
		fmt.Println(string(colorGreen), "\n\n\u2713 Migrated", i, "/", len(devices), "devices!", string(colorReset))
	} else {
		fmt.Println(string(colorRed), "\n\n\u2715 Failed to migrate all devices. Migrated", i, "/", len(devices), "devices!", string(colorReset))
	}
}

func updateDevice(device *gcpiotpb.Device) error {
	transformedDevice := map[string]interface{}{
		"device":      transform(device),
		"update_keys": Args.updatePublicKeys,
	}

	postBody, _ := json.Marshal(transformedDevice)
	responseBody := bytes.NewBuffer(postBody)

	url := Args.platformURL + "/api/v/1/code/" + Args.systemKey + "/devicesPatch"
	req, err := http.NewRequest("POST", url, responseBody)

	req.Header.Set("ClearBlade-UserToken", Args.token)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsonStr := string(body)
	var jsonMap map[string]interface{}

	if err := json.Unmarshal([]byte(jsonStr), &jsonMap); err != nil {
		// log.Fatalln("Unable to unmarshall JSON: ", err)
		return errors.New(jsonStr)
	}

	if jsonMap["error"] != nil {
		return errors.New(jsonStr)
	}

	return nil
}

func createDevice(device *gcpiotpb.Device) error {
	transformedDevice := transform(device)
	postBody, _ := json.Marshal(transformedDevice)
	responseBody := bytes.NewBuffer(postBody)
	url := Args.platformURL + "/api/v/1/code/" + Args.systemKey + "/devicesCreate"
	req, err := http.NewRequest("POST", url, responseBody)
	req.Header.Set("ClearBlade-UserToken", Args.token)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsonStr := string(body)
	var jsonMap map[string]interface{}

	if err := json.Unmarshal([]byte(jsonStr), &jsonMap); err != nil {
		// log.Fatalln("Unable to unmarshall JSON: ", err)
		return errors.New(jsonStr)
	}

	if jsonMap["error"] != nil {
		return errors.New(jsonStr)
	}

	return nil
}

func updateConfigHistory(deviceConfigs map[string]interface{}) error {
	transformedDeviceConfigHistory := map[string]interface{}{"configs": deviceConfigs}
	postBody, _ := json.Marshal(transformedDeviceConfigHistory)
	responseBody := bytes.NewBuffer(postBody)

	url := Args.platformURL + "/api/v/1/code/" + Args.systemKey + "/devicesConfigHistoryUpdate"
	req, err := http.NewRequest("POST", url, responseBody)
	req.Header.Set("ClearBlade-UserToken", Args.token)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	jsonStr := string(body)
	var jsonMap map[string]interface{}

	if err := json.Unmarshal([]byte(jsonStr), &jsonMap); err != nil {
		// log.Fatalln("Unable to unmarshall JSON: ", err)
		return errors.New(jsonStr)
	}

	if jsonMap["error"] != nil {
		return errors.New(jsonStr)
	}

	return nil
}
