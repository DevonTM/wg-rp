package client

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/DevonTM/wg-rp/pkg/bufferpool"

	"golang.zx2c4.com/wireguard/tun/netstack"
)

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
