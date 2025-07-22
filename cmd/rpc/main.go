package main

import (
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"

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
	var routeFlags arrayFlags
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
	interfaceIP, err := getInterfaceIP(string(config))
	if err != nil {
		log.Fatalf("Failed to get interface IP: %v", err)
	}

	// Extract MTU from config
	mtu, err := getMTU(string(config))
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
	ipcConfig, err := convertToIpcConfig(string(config))
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

	// Get the WireGuard interface IP to listen on
	wgIP, err := getWireGuardIP(string(config))
	if err != nil {
		log.Fatalf("Failed to get WireGuard IP: %v", err)
	}

	// Start route listeners
	var wg sync.WaitGroup
	for _, mapping := range mappings {
		wg.Add(1)
		go func(m RouteMapping) {
			defer wg.Done()
			startRouteListener(m, wgIP, tnet)
		}(mapping)
	}

	wg.Wait()
}

func getWireGuardIP(config string) (string, error) {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Address") {
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				continue
			}
			addr := strings.TrimSpace(parts[1])
			// Remove CIDR notation if present
			if strings.Contains(addr, "/") {
				addr = strings.Split(addr, "/")[0]
			}
			return addr, nil
		}
	}
	return "", fmt.Errorf("no Address found in WireGuard config")
}

func parsePort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("Invalid port: %s", portStr)
	}
	return port
}

func startRouteListener(mapping RouteMapping, wgIP string, tnet *netstack.Net) {
	listenAddr := fmt.Sprintf("%s:%s", wgIP, mapping.RemotePort)
	listener, err := tnet.ListenTCP(&net.TCPAddr{
		IP:   net.ParseIP(wgIP),
		Port: parsePort(mapping.RemotePort),
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

// convertToIpcConfig converts a standard WireGuard config to IPC format
func convertToIpcConfig(config string) (string, error) {
	var ipcConfig strings.Builder
	lines := strings.Split(config, "\n")
	inInterface := false
	inPeer := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if line == "[Interface]" {
			inInterface = true
			inPeer = false
			continue
		} else if line == "[Peer]" {
			inInterface = false
			inPeer = true
			continue
		}

		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			if inInterface {
				switch key {
				case "PrivateKey":
					// Convert base64 to hex
					keyBytes, err := base64.StdEncoding.DecodeString(value)
					if err != nil {
						return "", fmt.Errorf("failed to decode private key: %v", err)
					}
					hexKey := hex.EncodeToString(keyBytes)
					ipcConfig.WriteString(fmt.Sprintf("private_key=%s\n", hexKey))
				case "ListenPort":
					ipcConfig.WriteString(fmt.Sprintf("listen_port=%s\n", value))
				}
			} else if inPeer {
				switch key {
				case "PublicKey":
					// Convert base64 to hex
					keyBytes, err := base64.StdEncoding.DecodeString(value)
					if err != nil {
						return "", fmt.Errorf("failed to decode public key: %v", err)
					}
					hexKey := hex.EncodeToString(keyBytes)
					ipcConfig.WriteString(fmt.Sprintf("public_key=%s\n", hexKey))
				case "AllowedIPs":
					ipcConfig.WriteString(fmt.Sprintf("allowed_ip=%s\n", value))
				case "Endpoint":
					// Resolve hostname if present
					endpointValue := value
					if strings.Contains(value, ":") {
						host, port, err := net.SplitHostPort(value)
						if err != nil {
							return "", fmt.Errorf("failed to parse endpoint: %v", err)
						}

						// Try to resolve hostname to IP
						if net.ParseIP(host) == nil {
							ips, err := net.LookupIP(host)
							if err != nil {
								return "", fmt.Errorf("failed to resolve hostname %s: %v", host, err)
							}
							if len(ips) > 0 {
								endpointValue = net.JoinHostPort(ips[0].String(), port)
							}
						}
					}
					ipcConfig.WriteString(fmt.Sprintf("endpoint=%s\n", endpointValue))
				case "PersistentKeepalive":
					ipcConfig.WriteString(fmt.Sprintf("persistent_keepalive_interval=%s\n", value))
				}
			}
		}
	}

	return ipcConfig.String(), nil
}

// getInterfaceIP extracts the interface IP address from WireGuard config as netip.Addr
func getInterfaceIP(config string) (netip.Addr, error) {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Address") {
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				continue
			}
			addr := strings.TrimSpace(parts[1])
			// Remove CIDR notation if present
			if strings.Contains(addr, "/") {
				addr = strings.Split(addr, "/")[0]
			}

			ip, err := netip.ParseAddr(addr)
			if err != nil {
				return netip.Addr{}, fmt.Errorf("failed to parse IP address %s: %v", addr, err)
			}
			return ip, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no Address found in WireGuard config")
}

// getMTU extracts the MTU from WireGuard config
func getMTU(config string) (int, error) {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MTU") {
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				continue
			}
			mtuStr := strings.TrimSpace(parts[1])

			mtu := 1420 // default MTU
			if mtuStr != "" {
				var err error
				mtu, err = strconv.Atoi(mtuStr)
				if err != nil {
					return 0, fmt.Errorf("failed to parse MTU %s: %v", mtuStr, err)
				}
			}
			return mtu, nil
		}
	}
	// Return default MTU if not found
	return 1420, nil
}

// arrayFlags allows multiple flag values
type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}
