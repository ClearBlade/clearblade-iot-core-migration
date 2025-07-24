package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	cbiotcore "github.com/clearblade/go-iot"
)

func fetchDevices(service *cbiotcore.Service) []*cbiotcore.Device {
	checkpoint := GetCheckpoint()

	if checkpoint.IsPhaseCompleted(PhaseDeviceFetch) {
		printfColored(colorGreen, "\u2713 Device fetch phase already completed, loading from checkpoint")
		return checkpoint.GetFetchedDevices()
	}

	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	if Args.devicesCsvFile != "" {
		return fetchDevicesFromCSV(deviceService, Args.devicesCsvFile)
	}
	return fetchAllDevices(deviceService)
}

func fetchDevicesFromCSV(service *cbiotcore.ProjectsLocationsRegistriesDevicesService, csvFile string) []*cbiotcore.Device {
	var deviceMutex sync.Mutex
	var devices []*cbiotcore.Device

	checkpoint := GetCheckpoint()
	csvData, err := readCsvFile(csvFile)
	if err != nil {
		log.Fatal(err)
	}
	deviceIds := parseDeviceIds(csvData)

	remainingDeviceIds := checkpoint.GetUnfetchedDeviceIds(deviceIds)
	if len(remainingDeviceIds) == 0 {
		printfColored(colorGreen, " \u2713 All CSV devices already fetched from checkpoint")
		return checkpoint.GetFetchedDevices()
	}

	bar := getProgressBar(len(remainingDeviceIds), "Fetching remaining devices from source registry...")
	defer bar.Finish()
	wp := NewWorkerPool()
	wp.Run()

	for _, deviceId := range remainingDeviceIds {
		wp.AddTask(func() {
			device, err := service.Get(getCBSourceDevicePath(deviceId)).Do()
			if err != nil {
				log.Fatalln("Error fetching csv device: ", err.Error())
			}
			deviceMutex.Lock()
			defer deviceMutex.Unlock()
			devices = append(devices, device)
			checkpoint.AddFetchedDevice(device)
			bar.Add(1)
		})
	}

	wp.Wait()
	checkpoint.SetPhase(PhaseDeviceMigrate)
	printfColored(colorGreen, " \u2713 Done fetching devices")
	return devices
}

func fetchAllDevices(service *cbiotcore.ProjectsLocationsRegistriesDevicesService) []*cbiotcore.Device {
	checkpoint := GetCheckpoint()
	req := service.List(getCBSourceRegistryPath()).PageSize(Args.pageSize)
	devices, err := paginatedFetch(req, "Fetching all devices from source registry...")
	if err != nil {
		log.Fatalln("Error fetching all devices: ", err)
	}

	checkpoint.SetTotalDevices(len(devices))
	for _, device := range devices {
		checkpoint.AddFetchedDevice(device)
	}
	checkpoint.SetPhase(PhaseDeviceMigrate)

	printfColored(colorGreen, " \u2713 Done fetching devices")
	return devices
}

func fetchConfigHistory(service *cbiotcore.Service, devices []*cbiotcore.Device) map[string][]*cbiotcore.DeviceConfig {
	if !Args.configHistory {
		return nil
	}

	checkpoint := GetCheckpoint()
	remainingDevices := checkpoint.GetRemainingDevicesForConfig(devices)
	if len(remainingDevices) == 0 {
		printfColored(colorGreen, "\u2713 All device config history already fetched")
		return checkpoint.GetConfigHistory()
	}

	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)

	bar := getProgressBar(len(remainingDevices), "Fetching remaining device config history from source registry...")
	defer bar.Finish()

	wp := NewWorkerPool()
	wp.Run()

	for _, device := range remainingDevices {
		wp.AddTask(func() {
			dConfig, err := fetchConfigVersionHistory(device, deviceService)
			if err != nil {
				log.Fatalln("Error fetching config history: ", err)
			}

			checkpoint.AddProcessedConfig(device.Id, dConfig)
			bar.Add(1)
		})
	}

	wp.Wait()
	printfColored(colorGreen, " \u2713 Done fetching device configuration history")
	return checkpoint.GetConfigHistory()
}

func fetchConfigVersionHistory(device *cbiotcore.Device, service *cbiotcore.ProjectsLocationsRegistriesDevicesService) ([]*cbiotcore.DeviceConfig, error) {
	req := service.ConfigVersions.List(getCBSourceDevicePath(device.Id))
	resp, err := req.Do()
	if err != nil {
		return nil, err
	}
	return resp.DeviceConfigs, nil
}

func fetchGatewayBindings(service *cbiotcore.Service, devices []*cbiotcore.Device) map[string][]*cbiotcore.Device {
	var gateways []*cbiotcore.Device
	for _, device := range devices {
		if device.GatewayConfig.GatewayType == "GATEWAY" {
			gateways = append(gateways, device)
		}
	}
	if len(gateways) == 0 {
		return nil
	}
	bar := getProgressBar(len(devices), "Fetching gateways from source registry...")
	defer bar.Finish()
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	bindings := make(map[string][]*cbiotcore.Device, len(gateways))
	bindingMutex := sync.Mutex{}
	wp := NewWorkerPool()
	wp.Run()
	for _, gateway := range gateways {
		wp.AddTask(func() {
			req := deviceService.List(getCBSourceRegistryPath()).GatewayListOptionsAssociationsGatewayId(gateway.Id).PageSize(Args.pageSize)
			allBoundDevices, err := paginatedFetch(req, "")
			if err != nil {
				log.Fatalf("Error fetching gateways: %s\n", err)
			}

			bindingMutex.Lock()
			defer bindingMutex.Unlock()
			bindings[gateway.Id] = allBoundDevices
			bar.Add(1)
		})
	}
	wp.Wait()
	return bindings
}

func unbindFromGatewayIfAlreadyExistsInCBRegistry(gateway, parent string, cbDeviceService *cbiotcore.ProjectsLocationsRegistriesDevicesService, cbRegistryService *cbiotcore.ProjectsLocationsRegistriesService) {
	// fetch bound devices
	// if gateway doesn't exist -> do error checking and return
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
			log.Printf("Unable to unbind device %s from gateway %s. Reason: %s\n", boundDevices.Devices[i].Id, gateway, err.Error())
		}
	}
}

func migrateBoundDevicesToClearBlade(service *cbiotcore.Service, gatewayBindings map[string][]*cbiotcore.Device) {
	checkpoint := GetCheckpoint()

	if checkpoint.IsPhaseCompleted(PhaseGatewayBinding) {
		printfColored(colorGreen, "\u2713 Gateway binding phase already completed")
		return
	}

	if len(gatewayBindings) == 0 {
		checkpoint.SetPhase(PhaseComplete)
		return
	}

	remainingGateways := checkpoint.GetUnprocessedGateways(gatewayBindings)

	if len(remainingGateways) == 0 {
		printfColored(colorGreen, "\u2713 All gateways already processed")
		checkpoint.SetPhase(PhaseComplete)
		return
	}

	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)

	parent := getCBRegistryPath()

	bar := getProgressBar(len(remainingGateways), "Migrating remaining bound devices for gateways to destination registry...")
	defer bar.Finish()
	wp := NewWorkerPool()
	wp.Run()

	for _, gatewayID := range remainingGateways {
		boundDevices := gatewayBindings[gatewayID]
		wp.AddTask(func() {

			// First unbind any existing devices from the target gateway
			unbindFromGatewayIfAlreadyExistsInCBRegistry(gatewayID, parent, deviceService, registryService)

			// Process each bound device
			for _, device := range boundDevices {
				// Check if device exists in target registry
				_, err := deviceService.Get(getCBDevicePath(device.Id)).Do()
				if err != nil {
					if !strings.Contains(err.Error(), "Error 404") {
						errorLogger.AddError("Get Bound Device", device.Id, err)
						continue
					}

					// Create device if it doesn't exist
					_, createErr := deviceService.Create(parent, transform(device)).Do()
					if createErr != nil {
						errorLogger.AddError("Create Bound Device", device.Id, createErr)
						continue
					}
				}

				// Bind the device to the gateway
				bindDeviceResp, err := registryService.BindDeviceToGateway(parent, &cbiotcore.BindDeviceToGatewayRequest{
					DeviceId:  device.Id,
					GatewayId: gatewayID,
				}).Do()

				if err != nil {
					errorLogger.AddError("Bind device to gateway", device.Id, err)
					continue
				}

				if bindDeviceResp.ServerResponse.HTTPStatusCode != http.StatusOK {
					errorLogger.AddError("Bind device to gateway non-200 status", device.Id, err)
					continue
				}
			}

			checkpoint.AddProcessedGateway(gatewayID)
			bar.Add(1)
		})

	}
	wp.Wait()
	checkpoint.SetPhase(PhaseComplete)
	printfColored(colorGreen, "\u2713 Done migrating bound devices for gateways")
}

func addDevicesToClearBlade(service *cbiotcore.Service, devices []*cbiotcore.Device) int {
	checkpoint := GetCheckpoint()

	if checkpoint.IsPhaseCompleted(PhaseDeviceMigrate) {
		printfColored(colorGreen, "\u2713 Device migration phase already completed")
		return len(checkpoint.DevicesMigrated)
	}

	remainingDevices := checkpoint.GetRemainingDevicesForMigration(devices)
	if len(remainingDevices) == 0 {
		printfColored(colorGreen, "\u2713 All devices already migrated")
		checkpoint.SetPhase(PhaseConfigHistory)
		return len(checkpoint.DevicesMigrated)
	}

	bar := getProgressBar(len(remainingDevices), "Migrating remaining devices and gateways to destination registry...")
	defer bar.Finish()
	successfulCreates := newCounter()
	successfulCreates.SetCount(len(checkpoint.DevicesMigrated))
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)

	wp := NewWorkerPool()
	wp.Run()

	for _, device := range remainingDevices {
		wp.AddTask(func() {
			resp, err := deviceService.Create(getCBRegistryPath(), transform(device)).Do()
			if err == nil {
				// Create Device Successful
				successfulCreates.Increment()
				checkpoint.AddMigratedDevice(device.Id)
				bar.Add(1)
				return
			}

			// Checking if device exists - status code 409
			if !strings.Contains(err.Error(), "Error 409") {
				errorLogger.AddError("Create Device", device.Id, err)
				return
			}

			// Checking if network error
			if resp != nil && resp.ServerResponse.HTTPStatusCode != http.StatusConflict {
				errorLogger.AddError("Create Device", device.Id, err)
				return
			}

			// If Device exists, patch it
			err = updateDevice(deviceService, device)
			if err != nil {
				errorLogger.AddError("Patch Device", device.Id, err)
				return
			}

			successfulCreates.Increment()
			checkpoint.AddMigratedDevice(device.Id)
			bar.Add(1)
		})
	}

	wp.Wait()
	checkpoint.SetPhase(PhaseConfigHistory)
	return successfulCreates.Count()
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

func updateConfigHistory(service *cbiotcore.Service, deviceConfigs map[string][]*cbiotcore.DeviceConfig) error {
	checkpoint := GetCheckpoint()

	if checkpoint.IsPhaseCompleted(PhaseConfigHistory) {
		printfColored(colorGreen, "\u2713 Config history phase already completed")
		return nil
	}

	if len(deviceConfigs) == 0 {
		checkpoint.SetPhase(PhaseGatewayBinding)
		return nil
	}

	creds, _ := cbiotcore.GetRegistryCredentials(Args.cbRegistryName, Args.cbRegistryRegion, service)
	transformedDeviceConfigHistory := map[string]interface{}{"configs": deviceConfigs}
	postBody, _ := json.Marshal(transformedDeviceConfigHistory)
	responseBody := bytes.NewBuffer(postBody)

	url := creds.Url + "/api/v/1/code/" + creds.SystemKey + "/devicesConfigHistoryUpdate"
	req, err := http.NewRequest("POST", url, responseBody)
	if err != nil {
		return err
	}
	req.Header.Set("ClearBlade-UserToken", creds.Token)

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
		return errors.New(jsonStr)
	}

	if jsonMap["error"] != nil {
		return errors.New(jsonStr)
	}

	checkpoint.SetPhase(PhaseGatewayBinding)
	return nil
}

func deleteAllFromCbRegistry(service *cbiotcore.Service) {
	parent := getCBRegistryPath()
	cbDeviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)

	req := cbDeviceService.List(parent).GatewayListOptionsGatewayType("GATEWAY").PageSize(Args.pageSize)
	allGateways, err := paginatedFetch(req, "Fetching all gateways from destination registry...")
	if err != nil {
		log.Fatalln("Unable to list gateways from CB registry. Reason: ", err.Error())
	}
	printfColored(colorGreen, " \u2713 Done fetching gateways")

	func() {
		if len(allGateways) == 0 {
			return
		}
		progress := getProgressBar(len(allGateways), "Deleting gateways...")
		defer progress.Finish()
		for _, device := range allGateways {
			//Unbind devices from all gateways
			unbindFromGatewayIfAlreadyExistsInCBRegistry(device.Id, parent, cbDeviceService, registryService)
			//Delete all gateways
			if _, err := cbDeviceService.Delete(getCBDevicePath(device.Id)).Do(); err != nil {
				log.Fatalln("Unable to delete device from CB Registry: Reason: ", err.Error())
			}
			progress.Add(1)
		}
	}()
	printfColored(colorGreen, " \u2713 Done deleting gateways")

	req = cbDeviceService.List(parent).PageSize(Args.pageSize)
	allDevices, err := paginatedFetch(req, "Fetching all devices from destination registry...")
	if err != nil {
		log.Fatalln("Unable to list devices from CB registry. Reason: ", err.Error())
	}
	printfColored(colorGreen, " \u2713 Done fetching devices")

	func() {
		if len(allDevices) == 0 {
			return
		}
		wp := NewWorkerPool()
		wp.Run()
		progress := getProgressBar(len(allDevices), "Deleting devices from destination registry...")
		defer progress.Finish()
		for _, device := range allDevices {
			wp.AddTask(func() {
				if _, err := cbDeviceService.Delete(getCBDevicePath(device.Id)).Do(); err != nil {
					log.Fatalf("Unable to delete device from destination registry: %s\n", err)
				}
				progress.Add(1)
			})
		}
		wp.Wait()
	}()
	printfColored(colorGreen, " \u2713 Done deleting devices")
}
