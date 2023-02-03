package main

type Data struct {
	Project_id string `json:"project_id"`
}

type ErrorLog struct {
	Context  string
	Error    error
	DeviceId string
}
