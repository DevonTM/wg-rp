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

type ProxyMapping struct {
	ListenAddr string
	TargetIP   string
	TargetPort string
}

func main() {
	var configFile string
	var mappings []ProxyMapping

	flag.StringVar(&configFile, "c", "wg.conf", "WireGuard configuration file")

	// Custom flag for proxy mappings
	var proxyFlags utils.ArrayFlags
	flag.Var(&proxyFlags, "a", "Proxy mapping in format listen_ip:listen_port:target_ip (can be used multiple times)")

	flag.Parse()

	if len(proxyFlags) == 0 {
		log.Fatal("At least one proxy mapping (-a) must be specified")
	}

	// Parse proxy mappings
	for _, mapping := range proxyFlags {
		parts := strings.Split(mapping, ":")
		if len(parts) != 3 {
			log.Fatalf("Invalid proxy mapping format: %s. Expected format: listen_ip:listen_port:target_ip", mapping)
		}

		listenIP := parts[0]
		listenPort := parts[1]
		targetIP := parts[2]

		// If listen IP is empty, listen on all addresses
		var listenAddr string
		if listenIP == "" {
			listenAddr = ":" + listenPort
		} else {
			listenAddr = listenIP + ":" + listenPort
		}

		mappings = append(mappings, ProxyMapping{
			ListenAddr: listenAddr,
			TargetIP:   targetIP,
			TargetPort: listenPort, // Target port is same as listen port
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

	log.Printf("WireGuard server started with %d proxy mappings", len(mappings))

	// Start proxy servers
	var wg sync.WaitGroup
	for _, mapping := range mappings {
		wg.Add(1)
		go func(m ProxyMapping) {
			defer wg.Done()
			startProxyServer(m, tnet)
		}(mapping)
	}

	wg.Wait()
}

func startProxyServer(mapping ProxyMapping, tnet *netstack.Net) {
	listener, err := net.Listen("tcp", mapping.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", mapping.ListenAddr, err)
	}
	defer listener.Close()

	log.Printf("Proxy server listening on %s, forwarding to %s:%s",
		mapping.ListenAddr, mapping.TargetIP, mapping.TargetPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn, mapping, tnet)
	}
}

func handleConnection(clientConn net.Conn, mapping ProxyMapping, tnet *netstack.Net) {
	defer clientConn.Close()

	// Connect to target through WireGuard tunnel
	targetAddr := fmt.Sprintf("%s:%s", mapping.TargetIP, mapping.TargetPort)
	tunnelConn, err := tnet.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", targetAddr, err)
		return
	}
	defer tunnelConn.Close()

	log.Printf("Established connection: %s -> %s", clientConn.RemoteAddr(), targetAddr)

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
	log.Printf("Connection closed: %s -> %s", clientConn.RemoteAddr(), targetAddr)
}
