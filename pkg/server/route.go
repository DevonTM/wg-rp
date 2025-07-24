package server

import (
	"fmt"
	"log"
	"net"
	"sync"
)

// ProxyMapping represents an active port mapping
type ProxyMapping struct {
	LocalAddr  string
	RemotePort int
	ClientIP   string
	ClientPort int
	Listener   net.Listener
	cancel     chan struct{}
}

// handleMappingConnections handles incoming connections for a specific mapping
func (ps *ProxyServer) handleMappingConnections(mapping *ProxyMapping) {
	defer mapping.Listener.Close()

	for {
		select {
		case <-mapping.cancel:
			return
		default:
			conn, err := mapping.Listener.Accept()
			if err != nil {
				// Check if we're shutting down
				select {
				case <-mapping.cancel:
					return
				default:
					log.Printf("Failed to accept connection on port %d: %v", mapping.RemotePort, err)
					continue
				}
			}

			go ps.handleProxyConnection(conn, mapping)
		}
	}
}

// handleProxyConnection handles a single proxy connection
func (ps *ProxyServer) handleProxyConnection(clientConn net.Conn, mapping *ProxyMapping) {
	defer clientConn.Close()

	// Connect to client through WireGuard tunnel
	tunnelAddr := fmt.Sprintf("%s:%d", mapping.ClientIP, mapping.ClientPort)
	tunnelConn, err := ps.tnet.Dial("tcp", tunnelAddr)
	if err != nil {
		log.Printf("Failed to connect to client at %s:%d: %v", mapping.ClientIP, mapping.ClientPort, err)
		return
	}
	defer tunnelConn.Close()

	log.Printf("Established proxy connection: %s -> %s -> %s:%d -> %s",
		clientConn.RemoteAddr(), clientConn.LocalAddr(), mapping.ClientIP, mapping.ClientPort, mapping.LocalAddr)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ps.bufferPool.CopyWithBuffer(tunnelConn, clientConn)
		tunnelConn.Close()
	}()

	go func() {
		defer wg.Done()
		ps.bufferPool.CopyWithBuffer(clientConn, tunnelConn)
		clientConn.Close()
	}()

	wg.Wait()
	log.Printf("Proxy connection closed: %s -> %s -> %s:%d -> %s",
		clientConn.RemoteAddr(), clientConn.LocalAddr(), mapping.ClientIP, mapping.ClientPort, mapping.LocalAddr)
}

// removeClientMappings removes all port mappings for a specific client
func (ps *ProxyServer) removeClientMappings(clientIP string) {
	client, exists := ps.clients[clientIP]
	if !exists {
		return
	}

	// Close all mappings for this client
	for port := range client.Mappings {
		if mapping, exists := ps.mappings[port]; exists {
			close(mapping.cancel)
			mapping.Listener.Close()
			delete(ps.mappings, port)
			log.Printf("Removed stale port mapping for port %d (client %s)", port, clientIP)
		}
	}

	// Remove client from tracking
	delete(ps.clients, clientIP)
	log.Printf("Removed dead client %s and all its mappings", clientIP)
}
