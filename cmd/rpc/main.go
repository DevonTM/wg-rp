package main

import (
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"

	wgrp "wg-rp"
	"wg-rp/pkg/client"
	"wg-rp/pkg/utils"
	"wg-rp/pkg/wireguard"
)

func main() {
	var configFile string
	var verbose bool
	var showVersion bool

	flag.StringVar(&configFile, "c", "wg-client.conf", "WireGuard configuration file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging")
	flag.BoolVar(&showVersion, "V", false, "Show version and exit")

	// Custom flag for route mappings
	var routeFlags utils.ArrayFlags
	flag.Var(&routeFlags, "r", "Route mapping in format local_ip:local_port-remote_port (can be used multiple times)")

	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Printf("wg-rp client version %s\n", wgrp.VERSION)
		os.Exit(0)
	}

	// Print version on startup
	log.Printf("wg-rp client version %s starting...", wgrp.VERSION)

	if len(routeFlags) == 0 {
		log.Fatal("At least one route mapping (-r) must be specified")
	}

	// Read WireGuard config
	config, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file %s: %v", configFile, err)
	}

	// Initialize WireGuard device
	wgDevice, err := wireguard.NewWireGuardDevice(string(config), verbose)
	if err != nil {
		log.Fatalf("Failed to initialize WireGuard device: %v", err)
	}
	defer wgDevice.Close()

	// Determine server IP (first interface IP with different subnet)
	clientIP, serverIP, err := determineIPs(wgDevice.Config.InterfaceIPs)
	if err != nil {
		log.Fatalf("Failed to determine server IP: %v", err)
	}

	// Create proxy client
	proxyClient := client.NewProxyClient(wgDevice.Tnet, serverIP, clientIP)

	// Check if server is available before proceeding
	log.Printf("Checking server availability at %s...", serverIP)
	if err := proxyClient.CheckServerAvailability(); err != nil {
		log.Fatalf("Server is not available: %v", err)
	}
	log.Printf("Server is available and ready")

	// Parse and add route mappings
	routeMappings, err := client.ParseRouteMappings(routeFlags)
	if err != nil {
		log.Fatalf("Failed to parse route mappings: %v", err)
	}

	for _, mapping := range routeMappings {
		proxyClient.AddRouteMapping(mapping.LocalAddr, mapping.RemotePort)
	}

	log.Printf("WireGuard client started with %d route mappings", len(routeMappings))
	log.Printf("Client IPs: %v", wgDevice.Config.InterfaceIPs)
	log.Printf("Server IP: %s", serverIP)

	// Start the proxy client
	if err := proxyClient.Start(); err != nil {
		log.Fatalf("Failed to start proxy client: %v", err)
	}

	log.Printf("All route mappings active. Press Ctrl+C to exit.")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		// Wait for either server death or manual shutdown signal
		select {
		case <-proxyClient.WaitForShutdownSignal():
			log.Printf("Client stopped due to server shutdown")
		case <-sigChan:
			log.Printf("Received shutdown signal, cleaning up...")

			// Clean up port mappings
			if err := proxyClient.Cleanup(); err != nil {
				log.Printf("Error during cleanup: %v", err)
			}

			log.Printf("Cleanup completed. Exiting...")
			os.Exit(0)
		}
	}()

	// Wait for all route listeners
	proxyClient.Wait()
}

// determineIPs determines the client and server IPs based on the provided client IPs.
// For IPv4, it assumes the server is .1 in the same subnet.
// For IPv6, it assumes the server is ::1 in the same subnet.
func determineIPs(clientIPs []netip.Addr) (clientIP, serverIP string, err error) {
	for _, ip := range clientIPs {
		ipStr := ip.String()
		if ip.Is4() {
			parts := strings.Split(ipStr, ".")
			if len(parts) == 4 {
				serverIP = fmt.Sprintf("%s.%s.%s.1", parts[0], parts[1], parts[2])
				clientIP = ipStr
				return clientIP, serverIP, nil
			}
		} else if ip.Is6() {
			parts := strings.Split(ipStr, "::")
			if len(parts) >= 2 && parts[0] != "" {
				serverIP = fmt.Sprintf("[%s::1]", parts[0])
				clientIP = fmt.Sprintf("[%s]", ipStr)
				return clientIP, serverIP, nil
			}

		}
	}
	return "", "", fmt.Errorf("could not determine client and server IPs from: %v", clientIPs)
}
