// Handles registries operations
package main

import (
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
	base, err := url.Parse("https://iot.clearblade.com/api/v/4/webhook/execute/" + Args.systemKey + "/cloudiot")
	val, _ := getAbsPath(Args.serviceAccountFile)
	parent := "projects/" + getProjectID(val) + "/locations/" + Args.gcpRegistryRegion
	//Query params
	params := url.Values{}
	params.Add("parent", parent)
	base.RawQuery = params.Encode()
	req, err := http.NewRequest("GET", base.String(), nil)
	req.Header.Set("Clearblade-UserToken", Args.token)
	if err != nil {
		log.Print(err, "Error while preparing the request to list registries on clearblade with the following request:", req)
		os.Exit(1)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Print(err, "Error while listing registries on clearblade with the following request:", req)
	}
	return err, resp
}
