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

// GetInterfaceIP extracts the interface IP address from WireGuard config as netip.Addr
func GetInterfaceIP(config string) (netip.Addr, error) {
	lines := strings.SplitSeq(config, "\n")
	for line := range lines {
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

// GetMTU extracts the MTU from WireGuard config
func GetMTU(config string) (int, error) {
	lines := strings.SplitSeq(config, "\n")
	for line := range lines {
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

// ConvertConfigToIPC converts WireGuard .conf format to IPC format
func ConvertConfigToIPC(config string) (string, error) {
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
