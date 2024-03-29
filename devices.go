package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"

	gcpiotcore "cloud.google.com/go/iot/apiv1"
	gcpiotpb "cloud.google.com/go/iot/apiv1/iotpb"
	cbiotcore "github.com/clearblade/go-iot"
	"golang.org/x/exp/maps"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
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
	if Args.devicesCsvFile != "" {
		return fetchDevicesFromCSV(ctx, gcpClient)
	}

	return fetchAllDevices(ctx, gcpClient)
}

func fetchDevicesFromCSV(ctx context.Context, client *gcpiotcore.DeviceManagerClient) ([]*gcpiotpb.Device, map[string]interface{}) {

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
				Parent:    getGCPRegistryPath(),
				DeviceIds: batchDeviceIds,
				FieldMask: fields,
			}
			devicesSubset, devicesSubsetConfigHistory := fetchDevices(req, ctx, client, batchDeviceIds)
			devices = append(devices, devicesSubset...)
			maps.Copy(deviceConfigs, devicesSubsetConfigHistory)
		}

		return devices, deviceConfigs
	}

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    getGCPRegistryPath(),
		DeviceIds: deviceIds,
		FieldMask: fields,
	}

	devices, deviceConfigs = fetchDevices(req, ctx, client, deviceIds)

	return devices, deviceConfigs
}

func fetchAllDevices(ctx context.Context, client *gcpiotcore.DeviceManagerClient) ([]*gcpiotpb.Device, map[string]interface{}) {

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    getGCPRegistryPath(),
		FieldMask: fields,
	}

	var devices []*gcpiotpb.Device
	deviceConfigs := make(map[string]interface{})

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

		if Args.configHistory {
			deviceConfigs[resp.Id] = fetchConfigVersionHistory(resp, ctx, client)
		}

		if err := spinner.Add(1); err != nil {
			log.Fatalln("Unable to add to spinner: ", err)
		}
	}

	fmt.Println(string(colorGreen), "\u2713 Fetched", len(devices), "devices!", string(colorReset))
	return devices, deviceConfigs
}

func getMissingDeviceIds(devices []*gcpiotpb.Device, deviceIds []string) []string {
	missingDeviceIds := make([]string, 0)
	for _, id := range deviceIds {
		found := false
		for _, device := range devices {
			if device.Id == id {
				found = true
			}
		}
		if !found {
			missingDeviceIds = append(missingDeviceIds, id)
		}
	}
	return missingDeviceIds
}

func fetchDevices(req *gcpiotpb.ListDevicesRequest, ctx context.Context, client *gcpiotcore.DeviceManagerClient, deviceIds []string) ([]*gcpiotpb.Device, map[string]interface{}) {
	var devices []*gcpiotpb.Device
	deviceConfigs := make(map[string]interface{})
	devicesLength := len(deviceIds)

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

			successMsg := "Fetched " + fmt.Sprint(len(devices)) + " / " + fmt.Sprint(devicesLength) + " devices!"
			fmt.Println(string(colorGreen), "\n\u2713", successMsg, string(colorReset))
			if len(devices) != devicesLength {
				fmt.Printf("%sWarning: the following device IDs were not found - %s\n", string(colorYellow), strings.Join(getMissingDeviceIds(devices, deviceIds), ", "))
			}
			break
		}

		if err != nil {
			log.Fatalln("Unable to iterate over device records: ", err)
		}

		devices = append(devices, resp)
		if Args.configHistory {
			deviceConfigs[resp.Id] = fetchConfigVersionHistory(resp, ctx, client)
		}

		if err := bar.Add(1); err != nil {
			log.Fatalln("Unable to add to progressbar: ", err)
		}
	}
	return devices, deviceConfigs
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
			"binaryData":      base64.StdEncoding.EncodeToString(config.BinaryData),
		}
	}

	return configs
}

func unbindFromGatewayIfAlreadyExistsInCBRegistry(gateway, parent string, cbDeviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, cbRegistryService *cbiotcore.ProjectsLocationsRegistriesService) {
	// fetch bound devices
	// if gateway doesn't exists -> do error checking and return
	// if gateway exists, but no bound devices -> do check and return
	// if gateway exists and bound devices present -> unbind all devices & delete gateway

	boundDevices, err := cbDeviceService.List(parent).GatewayListOptionsAssociationsGatewayId(gateway).Do()

	if err != nil {
		log.Fatalln("Unable to fetch boundDevices for existing gateways from CB registry: ", err.Error())
	}

	if len(boundDevices.Devices) == 0 {
		return
	}

	for i := 0; i < len(boundDevices.Devices); i++ {
		_, err := cbRegistryService.UnbindDeviceFromGateway(parent, &cbiotcore.UnbindDeviceFromGatewayRequest{
			DeviceId:  boundDevices.Devices[i].Id,
			GatewayId: gateway,
		}).Do()

		if err != nil {
			fmt.Printf("Unable to unbind device %s from gateway %s. Reason: %s", boundDevices.Devices[i].Id, gateway, err.Error())
		}
	}
}

func migrateBoundDevicesToClearBlade(service *cbiotcore.Service, gcpIotClient *gcpiotcore.DeviceManagerClient, ctx context.Context, devices []*gcpiotpb.Device, errorLogs []ErrorLog) {
	gateways := make([]*gcpiotpb.Device, 0)
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)

	for i := 0; i < len(devices); i++ {
		if devices[i].GatewayConfig != nil && devices[i].GatewayConfig.GatewayType == *gcpiotpb.GatewayType_GATEWAY.Enum() {
			gateways = append(gateways, devices[i])
		}
	}

	if len(gateways) == 0 {
		return
	}

	fmt.Println()
	bar := getProgressBar(len(gateways), "Migrating bound devices for gateways...")

	parent := getCBRegistryPath()

	for _, gateway := range gateways {
		if barErr := bar.Add(1); barErr != nil {
			log.Fatalln("Unable to add to progressbar: ", barErr)
		}

		unbindFromGatewayIfAlreadyExistsInCBRegistry(gateway.Id, parent, deviceService, registryService)

		boundDevicesIterator := gcpIotClient.ListDevices(ctx, &gcpiotpb.ListDevicesRequest{
			Parent: getGCPRegistryPath(),
			GatewayListOptions: &gcpiotpb.GatewayListOptions{
				Filter: &gcpiotpb.GatewayListOptions_AssociationsGatewayId{
					AssociationsGatewayId: gateway.Id,
				},
			},
			FieldMask: fields,
		})

		for {
			resp, err := boundDevicesIterator.Next()
			if err == iterator.Done {
				break
			}

			if err != nil {
				errorLogs = append(errorLogs, ErrorLog{
					Context: "Bound Devices Iterator",
					Error:   err,
				})
				break
			}

			getDeviceResp, err := deviceService.Get(getCBDevicePath(resp.Id)).Do()
			if err != nil {
				if !strings.Contains(err.Error(), "Error 404") {
					errorLogs = append(errorLogs, ErrorLog{
						Context:  "Create Bound Device",
						Error:    err,
						DeviceId: resp.Id,
					})
					continue
				}

				_, createErr := deviceService.Create(parent, transform(resp)).Do()
				if createErr != nil {
					errorLogs = append(errorLogs, ErrorLog{
						Context:  "Create bound device",
						Error:    createErr,
						DeviceId: resp.Id,
					})
				}
			}

			bindDeviceResp, err := registryService.BindDeviceToGateway(parent, &cbiotcore.BindDeviceToGatewayRequest{
				DeviceId:  resp.Id,
				GatewayId: gateway.Id,
			}).Do()

			if err != nil {
				errorLogs = append(errorLogs, ErrorLog{
					Context:  "Bind device to gateway",
					Error:    err,
					DeviceId: resp.Id,
				})
				continue
			}

			if bindDeviceResp.ServerResponse.HTTPStatusCode != http.StatusOK {
				errorLogs = append(errorLogs, ErrorLog{
					Context:  "Bind device to gateway non-200 status",
					Error:    err,
					DeviceId: getDeviceResp.Id,
				})
				continue
			}
		}
	}
}

func addDevicesToClearBlade(service *cbiotcore.Service, devices []*gcpiotpb.Device, deviceConfigs map[string]interface{}, errorLogs []ErrorLog) []ErrorLog {
	bar := getProgressBar(len(devices), "Migrating Devices...")
	successfulCreates := 0

	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)

	wp := NewWorkerPool(TotalWorkers)
	wp.Run()

	resultC := make(chan ErrorLog, len(devices))

	for i := 0; i < len(devices); i++ {
		idx := i
		if barErr := bar.Add(1); barErr != nil {
			log.Fatalln("Unable to add to progressbar: ", barErr)
		}
		wp.AddTask(func() {
			resp, err := createDevice(deviceService, devices[idx])

			// Create Device Successful
			if err == nil {
				resultC <- ErrorLog{}
				return
			}

			// Checking if device exists - status code 409
			if !strings.Contains(err.Error(), "Error 409") {
				resultC <- ErrorLog{
					DeviceId: devices[idx].Id,
					Context:  "Error when Creating Device",
					Error:    err,
				}
				return
			}

			// Checking if network error
			if resp != nil && resp.ServerResponse.HTTPStatusCode != http.StatusConflict {
				resultC <- ErrorLog{
					DeviceId: devices[idx].Id,
					Context:  "Error when Creating Device",
					Error:    err,
				}
				return
			}

			// If Device exists, patch it
			err = updateDevice(deviceService, devices[idx])

			if err != nil {
				resultC <- ErrorLog{
					DeviceId: devices[idx].Id,
					Context:  "Error when Patching Device",
					Error:    err,
				}
				return
			}
			resultC <- ErrorLog{}
		})
	}

	for i := 0; i < len(devices); i++ {
		res := <-resultC
		if res.Error != nil {
			errorLogs = append(errorLogs, res)
		} else {
			successfulCreates += 1
		}
	}

	if len(deviceConfigs) != 0 {
		err := updateConfigHistory(service, deviceConfigs)
		if err != nil {
			fmt.Println(string(colorRed), "\n\n\u2715 Unable to update config version history! Reason: ", err, string(colorReset))
		}
	}

	if successfulCreates == len(devices) {
		fmt.Println(string(colorGreen), "\n\n\u2713 Migrated", successfulCreates, "/", len(devices), "devices!", string(colorReset))
	} else {
		fmt.Println(string(colorRed), "\n\n\u2715 Failed to migrate all devices. Migrated", successfulCreates, "/", len(devices), "devices!", string(colorReset))
	}

	return errorLogs
}

func updateDevice(deviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, device *gcpiotpb.Device) error {

	patchCall := deviceService.Patch(getCBDevicePath(device.Id), transform(device))

	if Args.updatePublicKeys {
		patchCall.UpdateMask("credentials,blocked,metadata,logLevel,gatewayConfig.gatewayAuthMethod")
	} else {
		patchCall.UpdateMask("blocked,metadata,logLevel,gatewayConfig.gatewayAuthMethod")
	}

	_, err := patchCall.Do()

	if err != nil {
		return err
	}

	if !Args.skipConfig {
		config := &cbiotcore.ModifyCloudToDeviceConfigRequest{
			VersionToUpdate: 0,
			BinaryData:      base64.StdEncoding.EncodeToString(device.Config.BinaryData),
		}

		updateConfigCall := deviceService.ModifyCloudToDeviceConfig(getCBDevicePath(device.Id), config)
		_, err := updateConfigCall.Do()

		if err != nil {
			return err
		}

		return nil
	}

	return nil

}

func createDevice(deviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, device *gcpiotpb.Device) (*cbiotcore.Device, error) {
	call := deviceService.Create(getCBRegistryPath(), transform(device))
	createDevResp, err := call.Do()
	return createDevResp, err
}

func updateConfigHistory(service *cbiotcore.Service, deviceConfigs map[string]interface{}) error {

	creds := cbiotcore.GetRegistryCredentials(Args.cbRegistryName, Args.cbRegistryRegion, service)

	transformedDeviceConfigHistory := map[string]interface{}{"configs": deviceConfigs}
	postBody, _ := json.Marshal(transformedDeviceConfigHistory)
	responseBody := bytes.NewBuffer(postBody)

	url := creds.Url + "/api/v/1/code/" + creds.SystemKey + "/devicesConfigHistoryUpdate"
	req, err := http.NewRequest("POST", url, responseBody)
	req.Header.Set("ClearBlade-UserToken", creds.Token)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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

func deleteAllFromCbRegistry(service *cbiotcore.Service) {
	//Delete all devices
	parent := getCBRegistryPath()
	cbDeviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)

	spinner := getSpinner("Cleaning Up ClearBlade Registry...")

	//FetchGateways
	resp, err := cbDeviceService.List(parent).GatewayListOptionsGatewayType("GATEWAY").PageSize(10000).Do()

	if err != nil {
		log.Fatalln("Unable to list gateways from CB registry. Reason: ", err.Error())
	}

	if len(resp.Devices) == 0 {
		return
	}

	for _, device := range resp.Devices {
		//Unbind devices from all gateways
		unbindFromGatewayIfAlreadyExistsInCBRegistry(device.Id, parent, cbDeviceService, registryService)
		//Delete all gateways
		if _, err := cbDeviceService.Delete(getCBDevicePath(device.Id)).Do(); err != nil {
			log.Fatalln("Unable to delete device from CB Registry: Reason: ", err.Error())
		}
		if err := spinner.Add(1); err != nil {
			log.Fatalln("Unable to add to spinner: ", err)
		}
	}

	resp, err = cbDeviceService.List(parent).PageSize(10000).Do()
	if err != nil {
		log.Fatalln("Unable to list devices from CB registry. Reason: ", err.Error())
	}

	for _, device := range resp.Devices {
		//Delete all devices
		if _, err := cbDeviceService.Delete(getCBDevicePath(device.Id)).Do(); err != nil {
			log.Fatalln("Unable to delete device from CB Registry: Reason: ", err.Error())
		}
		if err := spinner.Add(1); err != nil {
			log.Fatalln("Unable to add to spinner: ", err)
		}
	}
}
