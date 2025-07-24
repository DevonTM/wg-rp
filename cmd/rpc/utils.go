package main

import (
	"fmt"
	"net/netip"
	"strings"
)

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
