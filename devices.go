package main

import (
	gcpiotcore "cloud.google.com/go/iot/apiv1"
	"context"
	"fmt"
	cb "github.com/clearblade/Go-SDK"
	"google.golang.org/api/iterator"
	gcpiotpb "google.golang.org/genproto/googleapis/cloud/iot/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"log"
	"math"
	"time"
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

func fetchDevicesFromGoogleIotCore(ctx context.Context, gcpClient *gcpiotcore.DeviceManagerClient) []*gcpiotpb.Device {

	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := "projects/" + getProjectID(val) + "/locations/" + Args.region + "/registries/" + Args.registryName

	if Args.devicesCsvFile != "" {
		return fetchDevicesFromCSV(ctx, gcpClient, parent)
	}

	return fetchAllDevices(ctx, gcpClient, parent)
}

func fetchDevicesFromCSV(ctx context.Context, client *gcpiotcore.DeviceManagerClient, parent string) []*gcpiotpb.Device {

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
		defer client.Close()
		return devices
	}

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    parent,
		DeviceIds: deviceIds,
		FieldMask: fields,
	}

	devices = fetchDevices(req, ctx, client, len(deviceIds))
	defer client.Close()
	return devices
}

func fetchAllDevices(ctx context.Context, client *gcpiotcore.DeviceManagerClient, parent string) []*gcpiotpb.Device {

	req := &gcpiotpb.ListDevicesRequest{
		Parent:    parent,
		FieldMask: fields,
	}

	var devices []*gcpiotpb.Device
	it := client.ListDevices(ctx, req)
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

	defer client.Close()
	fmt.Println(string(colorGreen), "\n\u2713 Fetched", len(devices), "devices!", string(colorReset))
	return devices
}

func fetchDevices(req *gcpiotpb.ListDevicesRequest, ctx context.Context, client *gcpiotcore.DeviceManagerClient, devicesLength int) []*gcpiotpb.Device {
	var devices []*gcpiotpb.Device
	it := client.ListDevices(ctx, req)
	bar := getProgressBar(devicesLength, "Fetching devices from registry...", "")
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			if err := bar.Finish(); err != nil {
				log.Fatalln("Unable to finish progressbar: ", err)
			}
			successMsg := "Fetched " + fmt.Sprint(len(devices)) + " devices"
			fmt.Println(string(colorGreen), "\n\u2713 ", successMsg, string(colorReset))
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

func addDevicesToClearBlade(client *cb.DevClient, devices []*gcpiotpb.Device) {
	bar := getProgressBar(len(devices), "Migrating Devices...", "Migrated all devices!")
	for _, device := range devices {
		if barErr := bar.Add(1); barErr != nil {
			log.Fatalln("Unable to add to progressbar: ", barErr)
		}

		err := updateDevice(client, device)

		if err != nil {
			err := createDevice(client, device)
			if err != nil {
				log.Println("Unable to insert device: ", device.Id, ". Reason: ", err)
			} else {
				addPublicKeys(client, device.Id, device.Credentials)
			}
		} else {
			if Args.updatePublicKeys {
				query := cb.NewQuery()
				query.EqualTo("device_key", mkDeviceKey(Args.systemKey, device.Id))
				_, err := client.DeleteDevicePublicKey(Args.systemKey, device.Id, query)
				if err != nil {
					log.Println("Unable to delete public keys for: ", device.Id, ". Reason: ", err)
				} else {
					addPublicKeys(client, device.Id, device.Credentials)
				}
			}
		}
	}
}

func updateDevice(client *cb.DevClient, device *gcpiotpb.Device) error {
	_, err := client.UpdateDevice(Args.systemKey, device.Id, map[string]interface{}{
		"last_heart_beat_time":  device.LastHeartbeatTime.AsTime().Format(time.RFC3339),
		"last_event_time":       device.LastEventTime.AsTime().Format(time.RFC3339),
		"last_state_time":       device.LastStateTime.AsTime().Format(time.RFC3339),
		"last_config_ack_time":  device.LastConfigAckTime.AsTime().Format(time.RFC3339),
		"last_config_send_time": device.LastConfigSendTime.AsTime().Format(time.RFC3339),
		"blocked":               device.Blocked,
		"last_error_time":       device.LastErrorTime.AsTime().Format(time.RFC3339),
		"last_error_status":     device.LastErrorStatus,
		"config":                device.Config,
		"log_level":             device.LogLevel,
		"metadata":              device.Metadata,
		"gateway_config":        device.GatewayConfig,
	})

	return err
}

func createDevice(client *cb.DevClient, device *gcpiotpb.Device) error {
	_, err := client.CreateDevice(Args.systemKey, device.Id, map[string]interface{}{
		"last_heart_beat_time":   device.LastHeartbeatTime.AsTime().Format(time.RFC3339),
		"last_event_time":        device.LastEventTime.AsTime().Format(time.RFC3339),
		"last_state_time":        device.LastStateTime.AsTime().Format(time.RFC3339),
		"last_config_ack_time":   device.LastConfigAckTime.AsTime().Format(time.RFC3339),
		"last_config_send_time":  device.LastConfigSendTime.AsTime().Format(time.RFC3339),
		"blocked":                device.Blocked,
		"last_error_time":        device.LastErrorTime.AsTime().Format(time.RFC3339),
		"last_error_status":      device.LastErrorStatus,
		"config":                 device.Config,
		"log_level":              device.LogLevel,
		"metadata":               device.Metadata,
		"gateway_config":         device.GatewayConfig,
		"active_key":             generateRandomKey(),
		"enabled":                true,
		"allow_key_auth":         true,
		"allow_certificate_auth": true,
	})

	return err
}

func addPublicKeys(client *cb.DevClient, deviceId string, credentials []*gcpiotpb.DeviceCredential) {
	for _, creds := range credentials {
		_, err := client.AddDevicePublicKey(Args.systemKey, deviceId, creds.GetPublicKey().Key, getFormatNumber(creds.GetPublicKey().Format))
		if err != nil {
			log.Println("Unable to insert public key for: ", deviceId, ". Reason: ", err)
		}
	}
}
