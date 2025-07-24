package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"wg-rp/pkg/api"
	"wg-rp/pkg/utils"

	"golang.zx2c4.com/wireguard/tun/netstack"
)

// ProxyServer manages port mappings and proxy connections
type ProxyServer struct {
	tnet        *netstack.Net
	mappings    map[int]*ProxyMapping  // port -> mapping
	clients     map[string]*ClientInfo // clientIP -> client info
	mu          sync.RWMutex
	startupTime time.Time
}

// ClientInfo tracks information about connected clients
type ClientInfo struct {
	LastHeartbeat time.Time
	Mappings      map[int]bool // ports mapped by this client
}

// ProxyMapping represents an active port mapping
type ProxyMapping struct {
	LocalAddr  string
	RemotePort int
	ClientIP   string
	ClientPort int
	Listener   net.Listener
	cancel     chan struct{}
}

// NewProxyServer creates a new proxy server
func NewProxyServer(tnet *netstack.Net) *ProxyServer {
	return &ProxyServer{
		tnet:        tnet,
		mappings:    make(map[int]*ProxyMapping),
		clients:     make(map[string]*ClientInfo),
		startupTime: time.Now(),
	}
}

// StartAPIServer starts the REST API server on port 80 within the WireGuard netstack
func (ps *ProxyServer) StartAPIServer() error {
	mux := http.NewServeMux()

	// Heartbeat endpoint
	mux.HandleFunc("/api/v1/heartbeat", ps.handleHeartbeat)

	// Port mapping endpoints
	mux.HandleFunc("/api/v1/port-mappings", ps.handlePortMapping)

	listener, err := ps.tnet.ListenTCP(&net.TCPAddr{Port: 80})
	if err != nil {
		return fmt.Errorf("failed to listen on port 80: %v", err)
	}

	log.Printf("API server listening on :80 within WireGuard netstack")

	// Use Protocols to enable HTTP/2 support
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
		Protocols:    protocols,
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()

	return nil
}

// handlePortMapping handles port mapping requests
func (ps *ProxyServer) handlePortMapping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPost:
		ps.handleCreatePortMapping(w, r)
	case http.MethodDelete:
		ps.handleDeletePortMapping(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreatePortMapping creates a new port mapping
func (ps *ProxyServer) handleCreatePortMapping(w http.ResponseWriter, r *http.Request) {
	var req api.PortMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := api.PortMappingResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request body: %v", err),
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check if port is already mapped
	if _, exists := ps.mappings[req.RemotePort]; exists {
		response := api.PortMappingResponse{
			Success: false,
			Message: fmt.Sprintf("Port %d is already mapped", req.RemotePort),
		}
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Start listening on the requested port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", req.RemotePort))
	if err != nil {
		response := api.PortMappingResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to listen on port %d: %v", req.RemotePort, err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create mapping
	mapping := &ProxyMapping{
		LocalAddr:  req.LocalAddr,
		RemotePort: req.RemotePort,
		ClientIP:   req.ClientIP,
		ClientPort: req.ClientPort,
		Listener:   listener,
		cancel:     make(chan struct{}),
	}

	ps.mappings[req.RemotePort] = mapping

	// Track this mapping for the client
	client, exists := ps.clients[req.ClientIP]
	if !exists {
		client = &ClientInfo{
			Mappings: make(map[int]bool),
		}
		ps.clients[req.ClientIP] = client
	}
	client.Mappings[req.RemotePort] = true
	client.LastHeartbeat = time.Now() // Update heartbeat on mapping creation

	// Start handling connections for this mapping
	go ps.handleMappingConnections(mapping)

	log.Printf("Created port mapping: external:%d -> %s:%d -> %s",
		req.RemotePort, req.ClientIP, req.ClientPort, req.LocalAddr)

	response := api.PortMappingResponse{
		Success: true,
		Message: fmt.Sprintf("Port mapping created successfully for port %d", req.RemotePort),
	}
	json.NewEncoder(w).Encode(response)
}

// handleDeletePortMapping deletes an existing port mapping
func (ps *ProxyServer) handleDeletePortMapping(w http.ResponseWriter, r *http.Request) {
	portStr := r.URL.Query().Get("port")
	if portStr == "" {
		response := api.PortMappingResponse{
			Success: false,
			Message: "Port parameter is required",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		response := api.PortMappingResponse{
			Success: false,
			Message: "Invalid port number",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	mapping, exists := ps.mappings[port]
	if !exists {
		response := api.PortMappingResponse{
			Success: false,
			Message: fmt.Sprintf("No mapping found for port %d", port),
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Stop the mapping
	close(mapping.cancel)
	mapping.Listener.Close()
	delete(ps.mappings, port)

	// Remove from client tracking
	if client, exists := ps.clients[mapping.ClientIP]; exists {
		delete(client.Mappings, port)
	}

	log.Printf("Deleted port mapping for port %d", port)

	response := api.PortMappingResponse{
		Success: true,
		Message: fmt.Sprintf("Port mapping deleted successfully for port %d", port),
	}
	json.NewEncoder(w).Encode(response)
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
		io.Copy(tunnelConn, clientConn)
		tunnelConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, tunnelConn)
		clientConn.Close()
	}()

	wg.Wait()
	log.Printf("Proxy connection closed: %s -> %s -> %s:%d -> %s",
		clientConn.RemoteAddr(), clientConn.LocalAddr(), mapping.ClientIP, mapping.ClientPort, mapping.LocalAddr)
}

// StartHealthChecker starts a background goroutine that periodically checks client health
func (ps *ProxyServer) StartHealthChecker() {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		for range ticker.C {
			ps.checkClientHealth()
		}
	}()
}

// checkClientHealth checks if clients are still sending heartbeats and removes stale mappings
func (ps *ProxyServer) checkClientHealth() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	deadlineTimeout := 60 * time.Second // Consider client dead if no heartbeat for 60 seconds
	now := time.Now()

	var deadClients []string

	for clientIP, client := range ps.clients {
		if now.Sub(client.LastHeartbeat) > deadlineTimeout {
			timeSinceHeartbeat := now.Sub(client.LastHeartbeat)
			log.Printf("Client %s appears to be dead (no heartbeat for %s), removing all mappings",
				clientIP, utils.FormatDuration(timeSinceHeartbeat))
			deadClients = append(deadClients, clientIP)
		}
	}

	// Remove all mappings for dead clients
	for _, clientIP := range deadClients {
		ps.removeClientMappings(clientIP)
	}
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

// handleHeartbeat handles heartbeat requests from clients
func (ps *ProxyServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req api.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := api.HeartbeatResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request body: %v", err),
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Update or create client info
	client, exists := ps.clients[req.ClientIP]
	if !exists {
		client = &ClientInfo{
			Mappings: make(map[int]bool),
		}
		ps.clients[req.ClientIP] = client
	}

	client.LastHeartbeat = time.Now()

	response := api.HeartbeatResponse{
		Success:           true,
		Message:           "Heartbeat received",
		ServerStartupTime: ps.startupTime.Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
