package main

type Data struct {
	Project_id string `json:"project_id"`
}

type CbDevice struct {
	Blocked            bool                  `json:"blocked"`
	Config             DeviceConfig          `json:"config,omitempty"`
	Credentials        []CbDeviceCredential  `json:"credentials"`
	GatewayConfig      GatewayConfig         `json:"gatewayConfig,omitempty"`
	Id                 string                `json:"id"`
	LastConfigAckTime  string                `json:"lastConfigAckTime,omitempty"`
	LastConfigSendTime string                `json:"lastConfigSendTime,omitempty"`
	LastErrorStatus    DeviceLastErrorStatus `json:"lastErrorStatus"`
	LastErrorTime      string                `json:"lastErrorTime,omitempty"`
	LastEventTime      string                `json:"lastEventTime,omitempty"`
	LastHeartbeatTime  string                `json:"lastHeartbeatTime,omitempty"`
	LastStateTime      string                `json:"lastStateTime,omitempty"`
	LogLevel           string                `json:"logLevel,omitempty"`
	Metadata           map[string]string     `json:"metadata,omitempty"`
	Name               string                `json:"name"`
	NumId              string                `json:"numId"`
	State              DeviceState           `json:"state,omitempty"`
}

type CbDeviceCredential struct {
	ExpirationTime string                     `json:"expirationTime"`
	PublicKey      IoTCorePublicKeyCredential `json:"publicKey"`
}

type IoTCorePublicKeyCredential struct {
	Format string `json:"format,omitempty"`
	Key    string `json:"key,omitempty"`
}

type DeviceLastErrorStatus struct {
	Code    int32  `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type DeviceState struct {
	UpdateTime string `json:"updateTime,omitempty"`
	BinaryData string `json:"binaryData,omitempty"`
}

type DeviceConfig struct {
	Version         string `json:"version,omitempty"`
	CloudUpdateTime string `json:"cloudUpdateTime,omitempty"`
	DeviceAckTime   string `json:"deviceAckTime,omitempty"`
	BinaryData      string `json:"binaryData,omitempty"`
}

type GatewayConfig struct {
	GatewayType             string `json:"gatewayType,omitempty"`
	GatewayAuthMethod       string `json:"gatewayAuthMethod,omitempty"`
	LastAccessedGatewayId   string `json:"lastAccessedGatewayId,omitempty"`
	LastAccessedGatewayTime string `json:"lastAccessedGatewayTime,omitempty"`
}

type cbRegistries struct {
	DeviceRegistries []cbRegistry `json:"deviceRegistries"`
	NextPageToken    int          `json:"nextPageToken"`
}

type cbRegistry struct {
	Id                       string                    `json:"id"`
	Name                     string                    `json:"name,omitempty"`
	Credentials              any                       `json:"credentials"`
	EventNotificationConfigs []eventNotificationConfig `json:"eventNotificationConfigs,omitempty"`
	//EventNotificationConfigs any                      `json:"eventNotificationConfigs"`
	StateNotificationConfig *stateNotificationConfig `json:"stateNotificationConfig,omitempty"`
	HttpConfig              *httpEnabledState        `json:"httpConfig"`
	MqttConfig              *mqttEnabledState        `json:"mqttConfig"`
	LogLevel                any                      `json:"logLevel"`
}

type stateNotificationConfig struct {
	PubsubTopicName string `json:"pubsubTopicName"`
}

type eventNotificationConfig struct {
	PubsubTopicName  string      `json:"pubsubTopicName"`
	SubfolderMatches interface{} `json:"subfolderMatches,omitempty"`
}

type httpEnabledState struct {
	HttpEnabledState string `json:"httpEnabledState"`
}

type mqttEnabledState struct {
	MqttEnabledState string `json:"mqttEnabledState"`
}

type cbRegistryCredentials struct {
	SystemKey           string `json:"systemKey"`
	ServiceAccountToken string `json:"serviceAccountToken"`
	Url                 string `json:"url"`
}

type cbSystemCredentials struct {
	Project  string `json:"project"`
	Region   string `json:"region"`
	Registry string `json:"registry"`
}
