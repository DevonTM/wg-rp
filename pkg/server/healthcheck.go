package server

import (
	"log"
	"time"

	"wg-rp/pkg/utils"
)

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
