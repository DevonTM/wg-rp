package client

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
)

// RouteMapping represents a local to remote port mapping
type RouteMapping struct {
	LocalAddr  string // Format: ip:port (e.g., "127.0.0.1:8080")
	RemotePort int    // Port to expose on server
	ClientPort int    // Random port client listens on
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
