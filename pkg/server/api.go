package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"wg-rp/pkg/api"
)

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

	// Use Protocols to enable HTTP/1 and HTTP/2 cleartext support
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
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
	if mapping, exists := ps.mappings[req.RemotePort]; exists {
		// If the same client is trying to reclaim its own port, allow it by cleaning up the old mapping first
		if mapping.ClientIP == req.ClientIP {
			log.Printf("Client %s is reclaiming its own port %d, cleaning up old mapping", req.ClientIP, req.RemotePort)

			// Stop the existing mapping
			close(mapping.cancel)
			mapping.Listener.Close()
			delete(ps.mappings, req.RemotePort)

			// Remove from client tracking
			if client, exists := ps.clients[mapping.ClientIP]; exists {
				delete(client.Mappings, req.RemotePort)
			}
		} else {
			// Port is mapped by a different client
			response := api.PortMappingResponse{
				Success: false,
				Message: fmt.Sprintf("Port %d is already mapped by another client", req.RemotePort),
			}
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(response)
			return
		}
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

// handleHeartbeat handles heartbeat requests from clients
func (ps *ProxyServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req api.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := api.HeartbeatResponse{
			Success:           false,
			Message:           fmt.Sprintf("Invalid request body: %v", err),
			ServerStartupTime: ps.startupTime.Unix(),
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
