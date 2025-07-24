package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/DevonTM/wg-rp/pkg/api"
)

// registerPortMapping registers a port mapping with the server via REST API
func (pc *ProxyClient) registerPortMapping(mapping RouteMapping) error {
	request := api.PortMappingRequest{
		LocalAddr:  mapping.LocalAddr,
		RemotePort: mapping.RemotePort,
		ClientIP:   pc.clientIP,
		ClientPort: mapping.ClientPort,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	serverURL := fmt.Sprintf("http://%s/api/v1/port-mappings", pc.serverIP)
	resp, err := pc.httpClient.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	var response api.PortMappingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("server error: %s", response.Message)
	}

	log.Printf("Registered port mapping: remote port %d -> client port %d",
		mapping.RemotePort, mapping.ClientPort)
	return nil
}

// deletePortMapping deletes a port mapping from the server via REST API
func (pc *ProxyClient) deletePortMapping(remotePort int) error {
	serverURL := fmt.Sprintf("http://%s/api/v1/port-mappings?port=%d", pc.serverIP, remotePort)
	req, err := http.NewRequest(http.MethodDelete, serverURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := pc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	var response api.PortMappingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("server error: %s", response.Message)
	}

	log.Printf("Deleted port mapping for remote port %d", remotePort)
	return nil
}
