package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"wg-rp/pkg/api"
	"wg-rp/pkg/utils"
)

// startHeartbeat starts sending periodic heartbeats to the server
func (pc *ProxyClient) startHeartbeat() {
	go func() {
		ticker := time.NewTicker(20 * time.Second) // Send heartbeat every 20 seconds
		defer ticker.Stop()

		for {
			select {
			case <-pc.shutdownChan:
				log.Printf("Heartbeat stopped due to shutdown signal")
				return
			case <-ticker.C:
				if err := pc.sendHeartbeat(); err != nil {
					pc.heartbeatFailures++
					log.Printf("Failed to send heartbeat (attempt %d/%d): %v",
						pc.heartbeatFailures, pc.maxHeartbeatFails, err)

					if pc.heartbeatFailures >= pc.maxHeartbeatFails {
						log.Printf("Server appears to be dead after %d failed heartbeat attempts. Shutting down client...",
							pc.maxHeartbeatFails)

						// Signal shutdown to main application
						close(pc.shutdownChan)
						return
					}
				} else {
					// Reset failure counter on successful heartbeat
					pc.heartbeatFailures = 0
				}
			}
		}
	}()
}

// sendHeartbeat sends a heartbeat to the server
func (pc *ProxyClient) sendHeartbeat() error {
	request := api.HeartbeatRequest{
		ClientIP: pc.clientIP,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat request: %v", err)
	}

	serverURL := fmt.Sprintf("http://%s/api/v1/heartbeat", pc.serverIP)
	resp, err := pc.httpClient.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send heartbeat request: %v", err)
	}
	defer resp.Body.Close()

	var response api.HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode heartbeat response: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("heartbeat rejected: %s", response.Message)
	}

	// Check for server restart
	if pc.serverStartupTime != 0 && response.ServerStartupTime != pc.serverStartupTime {
		log.Printf("Server restart detected! Previous startup: %s, Current startup: %s",
			utils.FormatDateTimeFromUnix(pc.serverStartupTime), utils.FormatDateTimeFromUnix(response.ServerStartupTime))
		log.Printf("Re-registering all %d port mappings...", len(pc.mappings))

		// Re-register all port mappings
		for _, mapping := range pc.mappings {
			if err := pc.registerPortMapping(mapping); err != nil {
				log.Printf("Failed to re-register port mapping for port %d: %v", mapping.RemotePort, err)
				// Continue trying to register other mappings even if one fails
			}
		}
		log.Printf("Port mapping re-registration completed")
	}

	// Update the server startup time
	pc.serverStartupTime = response.ServerStartupTime

	return nil
}

// CheckServerAvailability checks if the server is available by sending a heartbeat
func (pc *ProxyClient) CheckServerAvailability() error {
	// Try to send a heartbeat to check server availability
	err := pc.sendHeartbeat()
	if err != nil {
		return fmt.Errorf("server heartbeat check failed: %v", err)
	}
	return nil
}
