package utils

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// WireGuardConfig holds parsed WireGuard configuration
type WireGuardConfig struct {
	InterfaceIPs []netip.Addr
	MTU          int
	IPCConfig    string
}

// ParseWireGuardConfig parses a WireGuard config file and returns all needed values in one pass
func ParseWireGuardConfig(config string) (*WireGuardConfig, error) {
	var interfaceIPs []netip.Addr
	var mtu int = 1420 // default MTU
	var ipcConfig strings.Builder

	lines := strings.SplitSeq(config, "\n")
	inInterface := false
	inPeer := false

	for line := range lines {
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
				case "Address":
					// Extract interface IP - handle multiple IPs (dual stack)
					addresses := strings.SplitSeq(value, ",")
					for addr := range addresses {
						addr = strings.TrimSpace(addr)
						if strings.Contains(addr, "/") {
							addr = strings.Split(addr, "/")[0]
						}

						ip, err := netip.ParseAddr(addr)
						if err != nil {
							return nil, fmt.Errorf("failed to parse IP address %s: %v", addr, err)
						}

						// Add to interfaceIPs slice
						interfaceIPs = append(interfaceIPs, ip)
					}
				case "MTU":
					// Extract MTU
					var err error
					mtu, err = strconv.Atoi(value)
					if err != nil {
						return nil, fmt.Errorf("failed to parse MTU %s: %v", value, err)
					}
				case "PrivateKey":
					// Convert base64 to hex for IPC
					keyBytes, err := base64.StdEncoding.DecodeString(value)
					if err != nil {
						return nil, fmt.Errorf("failed to decode private key: %v", err)
					}
					hexKey := hex.EncodeToString(keyBytes)
					ipcConfig.WriteString(fmt.Sprintf("private_key=%s\n", hexKey))
				case "ListenPort":
					// Validate UDP port range
					port, err := strconv.Atoi(value)
					if err != nil {
						return nil, fmt.Errorf("failed to parse ListenPort %s: %v", value, err)
					}
					if port < 1 || port > 65535 {
						return nil, fmt.Errorf("invalid ListenPort %d: must be between 1-65535", port)
					}
					ipcConfig.WriteString(fmt.Sprintf("listen_port=%s\n", value))
				}
			} else if inPeer {
				switch key {
				case "PublicKey":
					// Convert base64 to hex for IPC
					keyBytes, err := base64.StdEncoding.DecodeString(value)
					if err != nil {
						return nil, fmt.Errorf("failed to decode public key: %v", err)
					}
					hexKey := hex.EncodeToString(keyBytes)
					ipcConfig.WriteString(fmt.Sprintf("public_key=%s\n", hexKey))
				case "AllowedIPs":
					// Handle multiple IPs and ensure proper CIDR notation
					allowedIPs := strings.SplitSeq(value, ",")
					for allowedIP := range allowedIPs {
						allowedIP = strings.TrimSpace(allowedIP)

						// Add default prefix if not present
						if !strings.Contains(allowedIP, "/") {
							// Check if it's IPv4 or IPv6
							if ip := net.ParseIP(allowedIP); ip != nil {
								if ip.To4() != nil {
									allowedIP += "/32" // IPv4
								} else {
									allowedIP += "/128" // IPv6
								}
							} else {
								return nil, fmt.Errorf("invalid AllowedIP: %s", allowedIP)
							}
						}

						// Validate CIDR notation
						_, _, err := net.ParseCIDR(allowedIP)
						if err != nil {
							return nil, fmt.Errorf("invalid AllowedIP CIDR %s: %v", allowedIP, err)
						}

						ipcConfig.WriteString(fmt.Sprintf("allowed_ip=%s\n", allowedIP))
					}
				case "Endpoint":
					// Add default WireGuard port if not specified
					endpointValue := value
					if !strings.Contains(endpointValue, ":") {
						// No port specified, add default WireGuard port
						endpointValue = value + ":51820"
					}

					host, port, err := net.SplitHostPort(endpointValue)
					if err != nil {
						return nil, fmt.Errorf("failed to parse endpoint: %v", err)
					}

					// Validate port
					portNum, err := strconv.Atoi(port)
					if err != nil {
						return nil, fmt.Errorf("invalid endpoint port %s: %v", port, err)
					}
					if portNum < 1 || portNum > 65535 {
						return nil, fmt.Errorf("invalid endpoint port %d: must be between 1-65535", portNum)
					}

					// Try to resolve hostname to IP
					if net.ParseIP(host) == nil {
						ips, err := net.LookupIP(host)
						if err != nil {
							return nil, fmt.Errorf("failed to resolve hostname %s: %v", host, err)
						}
						if len(ips) > 0 {
							endpointValue = net.JoinHostPort(ips[0].String(), port)
						}
					}
					ipcConfig.WriteString(fmt.Sprintf("endpoint=%s\n", endpointValue))
				case "PersistentKeepalive":
					// Validate keepalive interval
					keepalive, err := strconv.Atoi(value)
					if err != nil {
						return nil, fmt.Errorf("failed to parse PersistentKeepalive %s: %v", value, err)
					}
					if keepalive < 0 || keepalive > 65535 {
						return nil, fmt.Errorf("invalid PersistentKeepalive %d: must be between 0-65535", keepalive)
					}
					ipcConfig.WriteString(fmt.Sprintf("persistent_keepalive_interval=%s\n", value))
				}
			}
		}
	}

	if len(interfaceIPs) == 0 {
		return nil, fmt.Errorf("no Address found in WireGuard config")
	}

	return &WireGuardConfig{
		InterfaceIPs: interfaceIPs,
		MTU:          mtu,
		IPCConfig:    ipcConfig.String(),
	}, nil
}

// ArrayFlags allows multiple flag values
type ArrayFlags []string

func (i *ArrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *ArrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// ParsePort converts a port string to int
func ParsePort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		panic(fmt.Sprintf("Invalid port: %s", portStr))
	}
	return port
}
