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
	"net/http"
	"strings"
	"sync"

	gcpiotpb "cloud.google.com/go/iot/apiv1/iotpb"
	cbiotcore "github.com/clearblade/go-iot"
)

func fetchDevicesFromClearBladeIotCore(ctx context.Context, service *cbiotcore.Service) ([]*cbiotcore.Device, map[string]interface{}) {
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	csvDevices := []*cbiotcore.Device{}
	if Args.devicesCsvFile != "" {
		csvDevices = fetchDevicesFromCSV(deviceService, Args.devicesCsvFile)
	}
	return fetchAllDevicesFromClearBlade(ctx, deviceService, csvDevices)
}

func fetchDevicesFromCSV(service *cbiotcore.ProjectsLocationsRegistriesDevicesService, csvFile string) []*cbiotcore.Device {
	var deviceMutex sync.Mutex
	var devices []*cbiotcore.Device

	csvData := readCsvFile(csvFile)
	deviceIds := parseDeviceIds(csvData)

	fmt.Println("Device IDs: ", deviceIds)

	wp := NewWorkerPool(TotalWorkers)
	wp.Run()

	for _, deviceId := range deviceIds {
		dId := deviceId
		wp.AddTask(func() {
			device, err := service.Get(getCBSourceDevicePath(dId)).Do()
			if err != nil {
				log.Fatalln("Error fetching csv device: ", err.Error())
			}
			deviceMutex.Lock()
			defer deviceMutex.Unlock()
			devices = append(devices, device)
		})
	}

	wp.Wait()

	return devices
}

func fetchAllDevicesFromClearBlade(ctx context.Context, service *cbiotcore.ProjectsLocationsRegistriesDevicesService, csvDevices []*cbiotcore.Device) ([]*cbiotcore.Device, map[string]interface{}) {
	var devices []*cbiotcore.Device
	configMutex := sync.Mutex{}
	deviceConfigs := make(map[string]interface{})
	fmt.Println()
	if len(csvDevices) != 0 {
		devices = csvDevices
	} else {
		spinner := getSpinner("Fetching all devices from registry...")
		req := service.List(getCBSourceRegistryPath()).PageSize(int64(1000))
		resp, err := req.Do()
		if err != nil {
			log.Fatalln("Error fetching all devices: ", err)
		}

		for resp.NextPageToken != "" {
			devices = append(devices, resp.Devices...)

			if err := spinner.Add(1); err != nil {
				log.Fatalln("Unable to add to spinner: ", err)
			}

			resp, err = req.PageToken(resp.NextPageToken).Do()

			if err != nil {
				log.Fatalln("Error fetching all devices: ", err.Error())
				break
			}
		}

		fmt.Println(string(colorGreen), "\n\u2713 Done fetching devices", string(colorReset))

		if err == nil {
			devices = append(devices, resp.Devices...)
		}
	}

	if Args.configHistory {
		fmt.Println("")
		bar := getProgressBar(len(devices), "Gathering Device Config History...")
		wp := NewWorkerPool(TotalWorkers)
		wp.Run()

		for _, device := range devices {
			d := device
			wp.AddTask(func() {
				dConfig := fetchConfigVersionHistory(d, ctx, service)
				configMutex.Lock()
				deviceConfigs[d.Id] = dConfig
				configMutex.Unlock()

				if err := bar.Add(1); err != nil {
					log.Fatalln("Unable to add to progressbar:", err)
				}
			})

		}

		wp.Wait()
		fmt.Println(string(colorGreen), "\n\u2713 Done fetching device configuration history", string(colorReset))

	}
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

func fetchConfigVersionHistory(device *cbiotcore.Device, _ context.Context, service *cbiotcore.ProjectsLocationsRegistriesDevicesService) map[string]interface{} {
	req := service.ConfigVersions.List(getCBSourceDevicePath(device.Id))
	resp, err := req.Do()
	if err != nil {
		log.Fatalln("fetchConfigVersionHistory ERROR: ", err)
	}

	configs := make(map[string]interface{})

	for _, config := range resp.DeviceConfigs {
		configs[fmt.Sprint(config.Version)] = map[string]interface{}{
			"cloudUpdateTime": config.CloudUpdateTime,
			"deviceAckTime":   config.DeviceAckTime,
			"binaryData":      base64.StdEncoding.EncodeToString([]byte(config.BinaryData)),
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

func migrateBoundDevicesToClearBlade(service *cbiotcore.Service, sourceService *cbiotcore.Service, sourceDevices []*cbiotcore.Device, errorLogs []ErrorLog) {
	gateways := make([]*cbiotcore.Device, 0)
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)
	sourceDeviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(sourceService)

	var errorLogMutex sync.Mutex

	// First identify all gateways
	for i := 0; i < len(sourceDevices); i++ {
		if sourceDevices[i].GatewayConfig != nil && sourceDevices[i].GatewayConfig.GatewayType == "GATEWAY" {
			gateways = append(gateways, sourceDevices[i])
		}
	}

	if len(gateways) == 0 {
		return
	}

	fmt.Println()
	bar := getProgressBar(len(gateways), "Migrating bound devices for gateways...")
	wp := NewWorkerPool(TotalWorkers)
	wp.Run()

	parent := getCBRegistryPath()
	sourceParent := getCBSourceRegistryPath()
	for _, gateway := range gateways {

		wp.AddTask(func() {
			if barErr := bar.Add(1); barErr != nil {
				log.Fatalln("Unable to add to progressbar: ", barErr)
			}

			// First unbind any existing devices from the target gateway
			unbindFromGatewayIfAlreadyExistsInCBRegistry(gateway.Id, parent, deviceService, registryService)

			// Fetch devices bound to this specific gateway from source
			boundDevices, err := sourceDeviceService.List(sourceParent).GatewayListOptionsAssociationsGatewayId(gateway.Id).PageSize(10000).Do()
			if err != nil {
				errorLogMutex.Lock()
				defer errorLogMutex.Unlock()
				errorLogs = append(errorLogs, ErrorLog{
					Context:  "Get bound devices for gateway",
					Error:    err,
					DeviceId: gateway.Id,
				})

				return
			}

			// Process each bound device
			for _, device := range boundDevices.Devices {
				// Check if device exists in target registry
				_, err := deviceService.Get(getCBDevicePath(device.Id)).Do()
				if err != nil {
					if !strings.Contains(err.Error(), "Error 404") {
						errorLogMutex.Lock()
						defer errorLogMutex.Unlock()
						errorLogs = append(errorLogs, ErrorLog{
							Context:  "Create Bound Device",
							Error:    err,
							DeviceId: device.Id,
						})
						continue
					}

					// Create device if it doesn't exist
					_, createErr := deviceService.Create(parent, transform(device)).Do()
					if createErr != nil {
						errorLogMutex.Lock()
						defer errorLogMutex.Unlock()
						errorLogs = append(errorLogs, ErrorLog{
							Context:  "Create bound device",
							Error:    createErr,
							DeviceId: device.Id,
						})
						continue
					}
				}

				// Bind the device to the gateway
				bindDeviceResp, err := registryService.BindDeviceToGateway(parent, &cbiotcore.BindDeviceToGatewayRequest{
					DeviceId:  device.Id,
					GatewayId: gateway.Id,
				}).Do()

				if err != nil {
					errorLogMutex.Lock()
					defer errorLogMutex.Unlock()
					errorLogs = append(errorLogs, ErrorLog{
						Context:  "Bind device to gateway",
						Error:    err,
						DeviceId: device.Id,
					})
					continue
				}

				if bindDeviceResp.ServerResponse.HTTPStatusCode != http.StatusOK {
					errorLogMutex.Lock()
					defer errorLogMutex.Unlock()
					errorLogs = append(errorLogs, ErrorLog{
						Context:  "Bind device to gateway non-200 status",
						Error:    err,
						DeviceId: device.Id,
					})
					continue
				}
			}
		})

	}
	wp.Wait()
	fmt.Println(string(colorGreen), "\n\u2713 Done migrating bound devices for gateways", string(colorReset))

}

func addDevicesToClearBlade(service *cbiotcore.Service, devices []*cbiotcore.Device, deviceConfigs map[string]interface{}, errorLogs []ErrorLog) []ErrorLog {
	fmt.Println("")
	bar := getProgressBar(len(devices), "Migrating Devices and Gateways...")
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

	wp.Wait()

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
		fmt.Println(string(colorGreen), "\n\n\u2713 Migrated", successfulCreates, "/", len(devices), "devices and gateways!", string(colorReset))
	} else {
		fmt.Println(string(colorRed), "\n\n\u2715 Failed to migrate all devices. Migrated", successfulCreates, "/", len(devices), "devices!", string(colorReset))
	}

	return errorLogs
}

func updateDevice(deviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, device *cbiotcore.Device) error {

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
			BinaryData:      base64.StdEncoding.EncodeToString([]byte(device.Config.BinaryData)),
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

func createDevice(deviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, device *cbiotcore.Device) (*cbiotcore.Device, error) {
	call := deviceService.Create(getCBRegistryPath(), transform(device))
	createDevResp, err := call.Do()
	return createDevResp, err
}

func updateConfigHistory(service *cbiotcore.Service, deviceConfigs map[string]interface{}) error {

	creds, _ := cbiotcore.GetRegistryCredentials(Args.cbRegistryName, Args.cbRegistryRegion, service)

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
