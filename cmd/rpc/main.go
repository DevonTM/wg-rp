package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"

	"wg-rp/pkg/utils"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

type RouteMapping struct {
	LocalAddr  string
	RemotePort string
}

func main() {
	var configFile string
	var mappings []RouteMapping

	flag.StringVar(&configFile, "c", "wg.conf", "WireGuard configuration file")

	// Custom flag for route mappings
	var routeFlags utils.ArrayFlags
	flag.Var(&routeFlags, "r", "Route mapping in format host:port (can be used multiple times)")

	flag.Parse()

	if len(routeFlags) == 0 {
		log.Fatal("At least one route mapping (-r) must be specified")
	}

	// Parse route mappings
	for _, mapping := range routeFlags {
		if !strings.Contains(mapping, ":") {
			log.Fatalf("Invalid route mapping format: %s. Expected format: host:port", mapping)
		}

		parts := strings.Split(mapping, ":")
		if len(parts) != 2 {
			log.Fatalf("Invalid route mapping format: %s. Expected format: host:port", mapping)
		}

		mappings = append(mappings, RouteMapping{
			LocalAddr:  mapping,
			RemotePort: parts[1],
		})
	}

	// Read WireGuard config
	config, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file %s: %v", configFile, err)
	}

	// Extract IP address from config
	interfaceIP, err := utils.GetInterfaceIP(string(config))
	if err != nil {
		log.Fatalf("Failed to get interface IP: %v", err)
	}

	// Extract MTU from config
	mtu, err := utils.GetMTU(string(config))
	if err != nil {
		log.Fatalf("Failed to get MTU: %v", err)
	}

	// Create netstack device with the interface IP and MTU
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{interfaceIP}, []netip.Addr{}, mtu)
	if err != nil {
		log.Fatalf("Failed to create netstack: %v", err)
	}

	// Create WireGuard device
	bind := conn.NewDefaultBind()
	dev := device.NewDevice(tun, bind, device.NewLogger(device.LogLevelVerbose, ""))

	// Configure the device
	ipcConfig, err := utils.ConvertConfigToIPC(string(config))
	if err != nil {
		log.Fatalf("Failed to convert config to IPC format: %v", err)
	}

	err = dev.IpcSet(ipcConfig)
	if err != nil {
		log.Fatalf("Failed to configure WireGuard device: %v", err)
	}

	// Bring up the device
	err = dev.Up()
	if err != nil {
		log.Fatalf("Failed to bring up WireGuard device: %v", err)
	}

	log.Printf("WireGuard client started with %d route mappings", len(mappings))

	// Start route listeners
	var wg sync.WaitGroup
	for _, mapping := range mappings {
		wg.Add(1)
		go func(m RouteMapping) {
			defer wg.Done()
			startRouteListener(m, interfaceIP.String(), tnet)
		}(mapping)
	}

	wg.Wait()
}

func startRouteListener(mapping RouteMapping, wgIP string, tnet *netstack.Net) {
	listenAddr := fmt.Sprintf("%s:%s", wgIP, mapping.RemotePort)
	listener, err := tnet.ListenTCP(&net.TCPAddr{
		IP:   net.ParseIP(wgIP),
		Port: utils.ParsePort(mapping.RemotePort),
	})
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	log.Printf("Route listener on %s, forwarding to %s", listenAddr, mapping.LocalAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleRouteConnection(conn, mapping)
	}
}

func handleRouteConnection(tunnelConn net.Conn, mapping RouteMapping) {
	defer tunnelConn.Close()

	// Connect to local service
	localConn, err := net.Dial("tcp", mapping.LocalAddr)
	if err != nil {
		log.Printf("Failed to connect to local service %s: %v", mapping.LocalAddr, err)
		return
	}
	defer localConn.Close()

	log.Printf("Established route connection: %s -> %s", tunnelConn.RemoteAddr(), mapping.LocalAddr)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(localConn, tunnelConn)
		localConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(tunnelConn, localConn)
		tunnelConn.Close()
	}()

	wg.Wait()
	log.Printf("Route connection closed: %s -> %s", tunnelConn.RemoteAddr(), mapping.LocalAddr)
}
