package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	wgrp "github.com/DevonTM/wg-rp"
	"github.com/DevonTM/wg-rp/pkg/server"
	"github.com/DevonTM/wg-rp/pkg/wireguard"
)

func main() {
	var configFile string
	var verbose bool
	var showVersion bool
	var bufferSizeKB int

	flag.StringVar(&configFile, "c", "wg-server.conf", "WireGuard configuration file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging on WireGuard device")
	flag.BoolVar(&showVersion, "V", false, "Show version and exit")
	flag.IntVar(&bufferSizeKB, "b", 64, "Buffer size for i/o operations (in KB, minimum 1KB)")
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Printf("wg-rp server version %s\n", wgrp.VERSION)
		os.Exit(0)
	}

	// Validate buffer size
	if bufferSizeKB < 1 {
		log.Fatal("Buffer size must be at least 1KB")
	}

	// Convert KB to bytes
	bufferSize := bufferSizeKB * 1024

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
	proxyServer := server.NewProxyServer(wgDevice.Tnet, bufferSize)

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
