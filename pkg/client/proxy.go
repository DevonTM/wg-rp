package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"wg-rp/pkg/api"
	"wg-rp/pkg/bufferpool"
	"wg-rp/pkg/utils"

	"golang.zx2c4.com/wireguard/tun/netstack"
)

// RouteMapping represents a local to remote port mapping
type RouteMapping struct {
	LocalAddr  string // Format: ip:port (e.g., "127.0.0.1:8080")
	RemotePort int    // Port to expose on server
	ClientPort int    // Random port client listens on
}

// ProxyClient manages client-side proxy connections
type ProxyClient struct {
	tnet              *netstack.Net
	serverIP          string
	clientIP          string
	mappings          []RouteMapping
	wg                sync.WaitGroup
	httpClient        *http.Client
	heartbeatFailures int
	maxHeartbeatFails int
	shutdownChan      chan struct{}
	serverStartupTime int64
	bufferPool        *bufferpool.BufferPool
}

// NewProxyClient creates a new proxy client
func NewProxyClient(tnet *netstack.Net, serverIP string, clientIP string, bufferSize int) *ProxyClient {
	// Use Protocols to enable HTTP/2 support
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	// Create HTTP client using the WireGuard netstack
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: tnet.DialContext,
			Protocols:   protocols,
		},
		Timeout: 10 * time.Second,
	}

	return &ProxyClient{
		tnet:              tnet,
		serverIP:          serverIP,
		clientIP:          clientIP,
		mappings:          make([]RouteMapping, 0),
		httpClient:        httpClient,
		maxHeartbeatFails: 3,
		shutdownChan:      make(chan struct{}),
		bufferPool:        bufferpool.NewBufferPool(bufferSize),
	}
}

// AddRouteMapping adds a route mapping configuration
func (pc *ProxyClient) AddRouteMapping(localAddr string, remotePort int) {
	// Generate a random port for the client listener
	clientPort := pc.generateRandomPort()

	mapping := RouteMapping{
		LocalAddr:  localAddr,
		RemotePort: remotePort,
		ClientPort: clientPort,
	}

	pc.mappings = append(pc.mappings, mapping)
	log.Printf("Added route mapping: %s <- %s:%d <- remote:%d",
		localAddr, pc.clientIP, clientPort, remotePort)
}

// Start starts all route listeners and registers them with the server
func (pc *ProxyClient) Start() error {
	// Start route listeners
	for _, mapping := range pc.mappings {
		pc.wg.Add(1)
		go func(m RouteMapping) {
			defer pc.wg.Done()
			pc.startRouteListener(m)
		}(mapping)
	}

	// Register port mappings with server
	for _, mapping := range pc.mappings {
		if err := pc.registerPortMapping(mapping); err != nil {
			log.Printf("Failed to register port mapping for port %d: %v", mapping.RemotePort, err)
			return err
		}
	}

	log.Printf("All %d route mappings registered successfully", len(pc.mappings))

	// Start sending heartbeats to the server
	pc.startHeartbeat()

	return nil
}

// Wait waits for all route listeners to finish
func (pc *ProxyClient) Wait() {
	pc.wg.Wait()
}

// generateRandomPort generates a random port number for internal use
func (pc *ProxyClient) generateRandomPort() int {
	for {
		// Use ports in range 10000-60000 to avoid conflicts
		port := 10000 + rand.IntN(50000)

		// Check if this port is already used in existing mappings
		used := false
		for _, mapping := range pc.mappings {
			if mapping.ClientPort == port {
				used = true
				break
			}
		}

		if !used {
			return port
		}
	}
}

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

// startRouteListener starts a listener for a specific route mapping
func (pc *ProxyClient) startRouteListener(mapping RouteMapping) {
	listener, err := pc.tnet.ListenTCP(&net.TCPAddr{Port: mapping.ClientPort})
	if err != nil {
		log.Fatalf("Failed to listen on client port %d: %v", mapping.ClientPort, err)
	}
	defer listener.Close()

	log.Printf("Route listener started on client port %d, forwarding to %s",
		mapping.ClientPort, mapping.LocalAddr)

	cancel := make(chan struct{})

	go func() {
		<-pc.shutdownChan
		listener.Close()
		close(cancel)
	}()

	for {
		select {
		case <-cancel:
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				if !pc.IsShuttingDown() {
					log.Printf("Failed to accept connection: %v", err)
				}
				continue
			}

			go pc.handleRouteConnection(conn, mapping)
		}
	}
}

// handleRouteConnection handles a single route connection
func (pc *ProxyClient) handleRouteConnection(tunnelConn net.Conn, mapping RouteMapping) {
	defer tunnelConn.Close()

	// Connect to local service
	localConn, err := net.Dial("tcp", mapping.LocalAddr)
	if err != nil {
		log.Printf("Failed to connect to local service %s: %v", mapping.LocalAddr, err)
		return
	}
	defer localConn.Close()

	log.Printf("Established route connection: %s <- %s <- %s <- remote:%d",
		mapping.LocalAddr, tunnelConn.LocalAddr(), tunnelConn.RemoteAddr(), mapping.RemotePort)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		pc.bufferPool.CopyWithBuffer(localConn, tunnelConn)
		localConn.Close()
	}()

	go func() {
		defer wg.Done()
		pc.bufferPool.CopyWithBuffer(tunnelConn, localConn)
		tunnelConn.Close()
	}()

	wg.Wait()
	log.Printf("Route connection closed: %s <- %s <- %s <- remote:%d",
		mapping.LocalAddr, tunnelConn.LocalAddr(), tunnelConn.RemoteAddr(), mapping.RemotePort)
}

// ParseRouteMappings parses route mapping strings in format "local_ip:local_port-remote_port"
func ParseRouteMappings(routeFlags []string) ([]RouteMapping, error) {
	var mappings []RouteMapping

	for _, mapping := range routeFlags {
		// Split by "-" to separate local and remote parts
		parts := strings.SplitN(mapping, "-", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid route mapping format: %s. Expected format: local_ip:local_port-remote_port", mapping)
		}

		localPart := parts[0]
		remotePortStr := parts[1]

		// Parse local part (ip:port)
		localHost, localPort, err := net.SplitHostPort(localPart)
		if err != nil {
			return nil, fmt.Errorf("invalid local address format: %s. Expected format: ip:port", localPart)
		}

		// Parse remote port
		remotePort, err := strconv.Atoi(remotePortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid remote port: %s", remotePortStr)
		}

		localAddr := net.JoinHostPort(localHost, localPort)
		mappings = append(mappings, RouteMapping{
			LocalAddr:  localAddr,
			RemotePort: remotePort,
		})
	}

	return mappings, nil
}

// Cleanup removes all port mappings from the server
func (pc *ProxyClient) Cleanup() error {
	log.Printf("Cleaning up %d port mappings...", len(pc.mappings))

	var lastErr error
	for _, mapping := range pc.mappings {
		if err := pc.deletePortMapping(mapping.RemotePort); err != nil {
			log.Printf("Failed to delete port mapping for port %d: %v", mapping.RemotePort, err)
			lastErr = err
		}
	}

	return lastErr
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

// WaitForShutdownSignal waits for either manual shutdown signal or server death
func (pc *ProxyClient) WaitForShutdownSignal() <-chan struct{} {
	return pc.shutdownChan
}

// IsShuttingDown returns true if the client is shutting down due to server failure
func (pc *ProxyClient) IsShuttingDown() bool {
	select {
	case <-pc.shutdownChan:
		return true
	default:
		return false
	}
}
