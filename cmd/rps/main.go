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
	var proxyFlags arrayFlags
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
	ipcConfig, err := convertConfigToIPC(string(config))
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

// convertConfigToIPC converts WireGuard .conf format to IPC format
func convertConfigToIPC(config string) (string, error) {
	var ipcConfig strings.Builder

	lines := strings.Split(config, "\n")
	inInterface := false
	inPeer := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
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

		// Parse key=value pairs
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}

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

// getInterfaceIP extracts the interface IP address from WireGuard config
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
