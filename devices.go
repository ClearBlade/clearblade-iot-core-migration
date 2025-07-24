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
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	if Args.devicesCsvFile != "" {
		return fetchDevicesFromCSV(deviceService, Args.devicesCsvFile)
	}
	return fetchAllDevices(deviceService)
}

func fetchDevicesFromCSV(service *cbiotcore.ProjectsLocationsRegistriesDevicesService, csvFile string) []*cbiotcore.Device {
	var deviceMutex sync.Mutex
	var devices []*cbiotcore.Device

	csvData, err := readCsvFile(csvFile)
	if err != nil {
		log.Fatal(err) // TODO
	}
	deviceIds := parseDeviceIds(csvData)

	bar := getProgressBar(len(deviceIds), "Fetching devices from source registry...")
	defer bar.Finish()
	wp := NewWorkerPool()
	wp.Run()

	for _, deviceId := range deviceIds {
		wp.AddTask(func() {
			device, err := service.Get(getCBSourceDevicePath(deviceId)).Do()
			if err != nil {
				log.Fatalln("Error fetching csv device: ", err.Error())
			}
			deviceMutex.Lock()
			defer deviceMutex.Unlock()
			devices = append(devices, device)
			bar.Add(1)
		})
	}

	wp.Wait()
	printfColored(colorGreen, " \u2713 Done fetching devices")
	return devices
}

func fetchAllDevices(service *cbiotcore.ProjectsLocationsRegistriesDevicesService) []*cbiotcore.Device {
	req := service.List(getCBSourceRegistryPath()).PageSize(Args.pageSize)
	devices, err := paginatedFetch(req, "Fetching all devices from source registry...")
	if err != nil {
		log.Fatalln("Error fetching all devices: ", err)
	}

	printfColored(colorGreen, " \u2713 Done fetching devices")
	return devices
}

func fetchConfigHistory(service *cbiotcore.Service, devices []*cbiotcore.Device) map[string]interface{} {
	if !Args.configHistory {
		return nil
	}

	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	configMutex := sync.Mutex{}
	deviceConfigs := make(map[string]interface{})

	bar := getProgressBar(len(devices), "Fetching device config history from source registry...")
	defer bar.Finish()

	wp := NewWorkerPool()
	wp.Run()

	for _, device := range devices {
		wp.AddTask(func() {
			dConfig, err := fetchConfigVersionHistory(device, deviceService)
			if err != nil {
				log.Fatalln("Error fetching config history: ", err)
			}

			configMutex.Lock()
			defer configMutex.Unlock()
			deviceConfigs[device.Id] = dConfig
			bar.Add(1)
		})
	}

	wp.Wait()
	printfColored(colorGreen, " \u2713 Done fetching device configuration history")
	return deviceConfigs
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
	if len(gatewayBindings) == 0 {
		return
	}
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)
	registryService := cbiotcore.NewProjectsLocationsRegistriesService(service)

	parent := getCBRegistryPath()

	bar := getProgressBar(len(gatewayBindings), "Migrating bound devices for gateways to destination registry...")
	defer bar.Finish()
	wp := NewWorkerPool()
	wp.Run()

	for gatewayID, boundDevices := range gatewayBindings {
		wp.AddTask(func() {
			bar.Add(1)

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
		})

	}
	wp.Wait()
	printfColored(colorGreen, "\u2713 Done migrating bound devices for gateways")
}

func addDevicesToClearBlade(service *cbiotcore.Service, devices []*cbiotcore.Device) int {
	bar := getProgressBar(len(devices), "Migrating devices and gateways to destination registry...")
	defer bar.Finish()
	successfulCreates := newCounter()
	deviceService := cbiotcore.NewProjectsLocationsRegistriesDevicesService(service)

	wp := NewWorkerPool()
	wp.Run()

	for _, device := range devices {
		wp.AddTask(func() {
			resp, err := deviceService.Create(getCBRegistryPath(), transform(device)).Do()
			if err == nil {
				// Create Device Successful
				successfulCreates.Increment()
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
			bar.Add(1)
		})
	}

	wp.Wait()
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

func updateConfigHistory(service *cbiotcore.Service, deviceConfigs map[string]interface{}) error {
	if len(deviceConfigs) == 0 {
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

	req = cbDeviceService.List(parent).PageSize(Args.pageSize)
	allDevices, err := paginatedFetch(req, "Fetching all devices from destination registry...")
	if err != nil {
		log.Fatalln("Unable to list devices from CB registry. Reason: ", err.Error())
	}

	func() {
		if len(allDevices) == 0 {
			return
		}
		progress := getProgressBar(len(allDevices), "Deleting devices from destination registry...")
		defer progress.Finish()
		for _, device := range allDevices {
			if _, err := cbDeviceService.Delete(getCBDevicePath(device.Id)).Do(); err != nil {
				log.Fatalf("Unable to delete device from destination registry: %s\n", err)
			}
		}
	}()
}
