package wireguard

import (
	"log"
	"net/netip"

	"wg-rp/pkg/config"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// WireGuardDevice wraps the WireGuard device and netstack
type WireGuardDevice struct {
	Device *device.Device
	Tnet   *netstack.Net
	Config *config.WireGuardConfig
}

// NewWireGuardDevice creates and configures a new WireGuard device
func NewWireGuardDevice(configData string, verbose bool) (*WireGuardDevice, error) {
	// Parse WireGuard config
	wgConfig, err := config.ParseWireGuardConfig(configData)
	if err != nil {
		return nil, err
	}

	// Create netstack device with the interface IP and MTU
	tun, tnet, err := netstack.CreateNetTUN(wgConfig.InterfaceIPs, []netip.Addr{}, wgConfig.MTU)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	// Bring up the device
	err = dev.Up()
	if err != nil {
		return nil, err
	}

	log.Printf("WireGuard device initialized with IPs: %v", wgConfig.InterfaceIPs)

	return &WireGuardDevice{
		Device: dev,
		Tnet:   tnet,
		Config: wgConfig,
	}, nil
}

// Close shuts down the WireGuard device
func (w *WireGuardDevice) Close() {
	if w.Device != nil {
		w.Device.Close()
	}
}
