package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	wgrp "wg-rp"
	"wg-rp/pkg/server"
	"wg-rp/pkg/wireguard"
)

func main() {
	var configFile string
	var verbose bool
	var showVersion bool

	flag.StringVar(&configFile, "c", "wg-server.conf", "WireGuard configuration file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging")
	flag.BoolVar(&showVersion, "V", false, "Show version and exit")
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Printf("wg-rp server version %s\n", wgrp.VERSION)
		os.Exit(0)
	}

	// Print version on startup
	log.Printf("wg-rp server version %s starting...", wgrp.VERSION)

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

	// Create proxy server
	proxyServer := server.NewProxyServer(wgDevice.Tnet)

	// Start API server
	if err := proxyServer.StartAPIServer(); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}

	// Start health checker for monitoring client connections
	proxyServer.StartHealthChecker()

	log.Printf("WireGuard proxy server started successfully")
	log.Printf("Server IPs: %v", wgDevice.Config.InterfaceIPs)
	log.Printf("API server running on port 80 within WireGuard netstack")
	log.Printf("Health checker started for monitoring client connections")
	log.Printf("Waiting for client connections...")

	// Keep the server running
	select {}
}
