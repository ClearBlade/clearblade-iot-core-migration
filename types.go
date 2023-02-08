package main

type GCPConfig struct {
	Project_id string `json:"project_id"`
}

type CBConfig struct {
	Project string `json:"project"`
}

type ErrorLog struct {
	Context  string
	Error    error
	DeviceId string
}
