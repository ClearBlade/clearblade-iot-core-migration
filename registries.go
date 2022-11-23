// Handles registries operations
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
)

// returns true if a registryName exists in the Clearblade project and region.
func registryExistsInClearBlade(registryName string) bool {
	err, resp := cbListRegistries()
	cbRegistries := parseListRegistriesJson(err, resp)
	var exists = false
	for _, registry := range cbRegistries.DeviceRegistries {
		if registry.Id == registryName {
			exists = true
			break
		}
	}
	return exists
}

// Parses the returned response from Clearblade List registries and returns cbRegistries.
func parseListRegistriesJson(err error, resp *http.Response) cbRegistries {
	var data cbRegistries
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error while while requesting registries from clearblade", err)
		os.Exit(1)
	}
	err2 := json.Unmarshal(body, &data)
	if err2 != nil {
		fmt.Println("Error while parsing json payload from clearblade", err2)
		os.Exit(1)
	}
	return data
}

// retrieves list of registries in clearblade by systemKey, gcpRegistryRegion, and projectId.
// TODO: this is temporary until the GetRegistry is fixed:
// https://github.com/ClearBlade/clearblade-iot-core-migration/issues/4
func cbListRegistries() (error, *http.Response) {
	base, err := url.Parse(iot_endpoint + Args.systemKey + "/cloudiot")
	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := "projects/" + getProjectID(val) + "/locations/" + Args.gcpRegistryRegion
	//Query params
	params := url.Values{}
	params.Add("parent", parent)
	base.RawQuery = params.Encode()
	req, err := http.NewRequest(http.MethodGet, base.String(), nil)
	req.Header.Set("Clearblade-UserToken", Args.token)
	if err != nil {
		log.Print(err, "Error while preparing the request to list registries in clearblade with the following request:", req)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err, "Error while listing registries in clearblade with the following request:", req)
	}
	return err, resp
}

// retrieves list of registries in clearblade by systemKey, gcpRegistryRegion, and projectId.
// TODO: this is temporary until the GetRegistry is fixed:
// https://github.com/ClearBlade/clearblade-iot-core-migration/issues/4
func cbCreateRegistry(pubsubTopicName string) (error, []byte) {
	fmt.Println(" Attempting to create the following registry", Args.registryName)
	base, err := url.Parse(iot_endpoint + Args.systemKey + "/cloudiot")
	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := "projects/" + getProjectID(val) + "/locations/" + Args.gcpRegistryRegion
	//Query params
	params := url.Values{}
	params.Add("parent", parent)
	base.RawQuery = params.Encode()

	registryData := &cbRegistry{
		Id:                       Args.registryName,
		Credentials:              make([]string, 0),
		EventNotificationConfigs: make([]string, 0),
		//StateNotificationConfig:  "{\"pubsubTopicName\":" + pubsubTopicName + "\"}", //TODO: populate this part from the user input
		StateNotificationConfig: &stateNotificationConfig{PubsubTopicName: ""}, //TODO: populate this part from the user input
		HttpConfig:              &httpEnabledState{HttpEnabledState: "HTTP_ENABLED"},
		MqttConfig:              &mqttEnabledState{MqttEnabledState: "MQTT_ENABLED"},
		LogLevel:                "NONE",
	}
	jsonBody, _ := json.Marshal(registryData)
	req, err := http.NewRequest(http.MethodPost, base.String(), bytes.NewBuffer(jsonBody))
	req.Header.Set("Clearblade-UserToken", Args.token)
	if err != nil {
		log.Print(err, "Error while preparing the request to create registry in clearblade with the following request:", req)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err, "Error while listing registries on clearblade with the following request:", req)
		os.Exit(1)
	}
	response, _ := io.ReadAll(resp.Body)
	fmt.Println(" Registry created: ", string(response))
	return err, response
}

// retrieves for a given clearblade cbRegistryName the following: systemKey,
// gcpRegistryRegion, and projectId and returns in a cbRegistryCredentials.
func fetchRegistryCredentials(cbRegistryName string) cbRegistryCredentials {
	fmt.Println(" Attempting to fetch from Clearblade the credentials of the following"+
		" registry: ", cbRegistryName)
	base, err := url.Parse(iot_endpoint_v1 + Args.systemKey + "/getRegistryCredentials")
	val, _ := getAbsPath(Args.serviceAccountFile)
	projectId := getProjectID(val)

	regionName := ""
	if len(Args.cbRegistryRegion) != 0 {
		regionName = Args.cbRegistryRegion
	} else {
		regionName = Args.gcpRegistryRegion
	}

	systemCredentials := &cbSystemCredentials{
		Project:  projectId,
		Region:   regionName,
		Registry: cbRegistryName,
	}
	jsonBody, _ := json.Marshal(systemCredentials)
	req, err := http.NewRequest(http.MethodPost, base.String(), bytes.NewBuffer(jsonBody))
	req.Header.Set("Clearblade-UserToken", Args.token)
	if err != nil {
		log.Print(err, "Error while preparing the request to fetch clearblade registry credentials with the following request:", req)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err, "Error while fetching registry from clearblade with the following request:", req)
		os.Exit(1)
	}

	fetchedCredentials := parseGetCbRegistryJson(err, resp)
	return fetchedCredentials
}

// Parses the returned response from Clearblade get registry credentials and returns cbRegistryCredentials.
func parseGetCbRegistryJson(err error, resp *http.Response) cbRegistryCredentials {
	var data cbRegistryCredentials
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error while while requesting registry credentials from clearblade", err)
		os.Exit(1)
	}
	err2 := json.Unmarshal(body, &data)
	if err2 != nil {
		fmt.Println("Error while parsing json payload from clearblade", err2)
		os.Exit(1)
	}
	log.Println("Fetched credentials from clearblade")
	return data
}
