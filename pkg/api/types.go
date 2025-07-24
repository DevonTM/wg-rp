package api

// PortMappingRequest represents a request to create a port mapping
type PortMappingRequest struct {
	LocalAddr  string `json:"local_addr"`  // Format: ip:port (e.g., "127.0.0.1:8080")
	RemotePort int    `json:"remote_port"` // Port to expose on server (e.g., 8080)
	ClientIP   string `json:"client_ip"`   // Client IP within WireGuard tunnel
	ClientPort int    `json:"client_port"` // Random port client is listening on
}

// PortMappingResponse represents the response to a port mapping request
type PortMappingResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// HeartbeatRequest represents a heartbeat request from client
type HeartbeatRequest struct {
	ClientIP string `json:"client_ip"` // Client IP within WireGuard tunnel
}

// HeartbeatResponse represents the response to a heartbeat request
type HeartbeatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
