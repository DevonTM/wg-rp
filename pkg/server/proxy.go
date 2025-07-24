package server

import (
	"sync"
	"time"

	"github.com/DevonTM/wg-rp/pkg/bufferpool"

	"golang.zx2c4.com/wireguard/tun/netstack"
)

// ProxyServer manages port mappings and proxy connections
type ProxyServer struct {
	tnet        *netstack.Net
	mappings    map[int]*ProxyMapping  // port -> mapping
	clients     map[string]*ClientInfo // clientIP -> client info
	mu          sync.RWMutex
	startupTime time.Time
	bufferPool  *bufferpool.BufferPool
}

// ClientInfo tracks information about connected clients
type ClientInfo struct {
	LastHeartbeat time.Time
	Mappings      map[int]bool // ports mapped by this client
}

// NewProxyServer creates a new proxy server
func NewProxyServer(tnet *netstack.Net, bufferSize int) *ProxyServer {
	return &ProxyServer{
		tnet:        tnet,
		mappings:    make(map[int]*ProxyMapping),
		clients:     make(map[string]*ClientInfo),
		startupTime: time.Now(),
		bufferPool:  bufferpool.NewBufferPool(bufferSize),
	}
}
