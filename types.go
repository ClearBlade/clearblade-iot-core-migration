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
	LastConfigAckTime  string                `json:"-"`
	LastConfigSendTime string                `json:"-"`
	LastErrorStatus    DeviceLastErrorStatus `json:"-"`
	LastErrorTime      string                `json:"-"`
	LastEventTime      string                `json:"-"`
	LastHeartbeatTime  string                `json:"-"`
	LastStateTime      string                `json:"-"`
	LogLevel           string                `json:"-"`
	Metadata           map[string]string     `json:"-"`
	Name               string                `json:"name"`
	NumId              string                `json:"numId"`
	State              DeviceState           `json:"-"`
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
