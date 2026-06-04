package client

import "net/http"

// WaterbutlerClient handles file uploads and downloads via the Waterbutler service.
// Base URL: https://files.osf.io
type WaterbutlerClient struct {
	token string
	http  *http.Client
}

// NewWaterbutler returns a WaterbutlerClient.
func NewWaterbutler(token string) *WaterbutlerClient {
	return &WaterbutlerClient{
		token: token,
		http:  &http.Client{Timeout: 0}, // no timeout for large file transfers
	}
}
