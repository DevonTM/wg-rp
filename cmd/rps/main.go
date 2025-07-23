package main

import (
	"flag"
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
	TargetAddr string
}

func main() {
	var configFile string
	var mappings []ProxyMapping
	var verbose bool

	flag.StringVar(&configFile, "c", "wg.conf", "WireGuard configuration file")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging")

	// Custom flag for proxy mappings
	var proxyFlags utils.ArrayFlags
	flag.Var(&proxyFlags, "a", "Proxy mapping in format listen_ip:listen_port-target_ip:target_port (can be used multiple times)")

	flag.Parse()

	if len(proxyFlags) == 0 {
		log.Fatal("At least one proxy mapping (-a) must be specified")
	}

	// Parse proxy mappings
	for _, mapping := range proxyFlags {
		// Split by "-" to separate listen and target parts
		parts := strings.SplitN(mapping, "-", 2)
		if len(parts) != 2 {
			log.Fatalf("Invalid proxy mapping format: %s. Expected format: listen_ip:listen_port-target_ip:target_port", mapping)
		}

		listenPart := parts[0]
		targetPart := parts[1]

		// Parse listen part (ip:port)
		listenHost, listenPort, err := net.SplitHostPort(listenPart)
		if err != nil {
			log.Fatalf("Invalid listen address format: %s. Expected format: ip:port or :port", listenPart)
		}

		// Parse target part (ip:port)
		targetHost, targetPort, err := net.SplitHostPort(targetPart)
		if err != nil {
			log.Fatalf("Invalid target address format: %s. Expected format: ip:port", targetPart)
		}

		mappings = append(mappings, ProxyMapping{
			ListenAddr: net.JoinHostPort(listenHost, listenPort),
			TargetAddr: net.JoinHostPort(targetHost, targetPort),
		})
	}

	// Read WireGuard config
	config, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file %s: %v", configFile, err)
	}

	// Parse WireGuard config in one pass
	wgConfig, err := utils.ParseWireGuardConfig(string(config))
	if err != nil {
		log.Fatalf("Failed to parse WireGuard config: %v", err)
	}

	// Create netstack device with the interface IP and MTU
	tun, tnet, err := netstack.CreateNetTUN(wgConfig.InterfaceIPs, []netip.Addr{}, wgConfig.MTU)
	if err != nil {
		log.Fatalf("Failed to create netstack: %v", err)
	}

	// Create WireGuard device
	bind := conn.NewDefaultBind()

	// Set log level based on verbose flag
	logLevel := device.LogLevelError
	if verbose {
		logLevel = device.LogLevelVerbose
	}

	dev := device.NewDevice(tun, bind, device.NewLogger(logLevel, ""))

	// Configure the device
	err = dev.IpcSet(wgConfig.IPCConfig)
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
	network := "tcp"
	laddr, err := net.ResolveTCPAddr(network, mapping.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to resolve listen address %s: %v", mapping.ListenAddr, err)
	}

	if strings.Split(mapping.ListenAddr, ":")[0] == "" {
		network = "tcp"
	} else if laddr.IP.To4() != nil {
		network = "tcp4"
	} else {
		network = "tcp6"
	}

	listener, err := net.ListenTCP(network, laddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", mapping.ListenAddr, err)
	}
	defer listener.Close()

	log.Printf("Proxy server listening on %s, forwarding to %s",
		mapping.ListenAddr, mapping.TargetAddr)

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
	tunnelConn, err := tnet.Dial("tcp", mapping.TargetAddr)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", mapping.TargetAddr, err)
		return
	}
	defer tunnelConn.Close()

	log.Printf("Established connection: %s -> %s", clientConn.RemoteAddr(), mapping.TargetAddr)

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
	log.Printf("Connection closed: %s -> %s", clientConn.RemoteAddr(), mapping.TargetAddr)
}
