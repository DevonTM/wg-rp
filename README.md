# WG-RP: WireGuard Reverse Proxy

A simplified WireGuard-based reverse proxy system similar to FRP (Fast Reverse Proxy) that allows exposing local services through a WireGuard tunnel without complex port configurations on the server side.

## Architecture

The new architecture eliminates the need for manual port configuration on the server side:

- **RPS (Server)**: Only needs WireGuard configuration, hosts a REST API within the WireGuard netstack on port 80
- **RPC (Client)**: Connects to RPS and dynamically registers port mappings via REST API
- **Dynamic Port Allocation**: Client uses random internal ports, server opens external ports on demand
- **Heartbeat Mechanism**: Client sends periodic heartbeats to maintain connection, server automatically cleans up stale mappings
- **Automatic Cleanup**: When client disconnects, all associated port mappings are automatically removed

## Components

### Core Packages

- `pkg/config/`: WireGuard configuration parsing
- `pkg/wireguard/`: WireGuard device management
- `pkg/server/`: Server-side proxy and API handling
- `pkg/client/`: Client-side proxy and API communication
- `pkg/api/`: Shared API types and structures
- `pkg/bufferpool/`: Efficient buffer pool for I/O operations
- `pkg/utils/`: Utility functions

### Binaries

- `rps`: Server binary (WireGuard Reverse Proxy Server)
- `rpc`: Client binary (WireGuard Reverse Proxy Client)

## Usage

### Server (RPS)

```bash
# Start server with default config
./bin/rps

# Start server with custom config and verbose logging
./bin/rps -c wg-server.conf -v

# Start server with custom buffer size (128KB for high-traffic scenarios)
./bin/rps -c wg-server.conf -b 128

# Show version
./bin/rps -V
```

The server:
1. Reads WireGuard configuration
2. Creates WireGuard netstack device
3. Starts REST API on port 80 within the WireGuard netstack
4. Starts heartbeat-based health checker
5. Waits for client connections and port mapping requests
6. Automatically cleans up mappings for disconnected clients

### Client (RPC)

```bash
# Expose local service localhost:8080 to server port 8080
./bin/rpc -r localhost:8080-8080

# Multiple port mappings
./bin/rpc -r localhost:8080-8080 -r localhost:3000-3000

# With custom config, verbose logging, and optimized buffer size
./bin/rpc -c wg-client.conf -v -b 128 -r localhost:8080-8080

# For high-throughput applications (file transfers, video streaming)
./bin/rpc -c wg-client.conf -b 256 -r localhost:8080-8080

# Show version
./bin/rpc -V
```

The client:
1. Reads WireGuard configuration
2. Creates WireGuard netstack device
3. Checks server availability before proceeding
4. Parses route mappings (format: `local_ip:local_port-remote_port`)
5. Starts internal listeners on random ports
6. Registers port mappings with server via REST API
7. Starts heartbeat mechanism to maintain connection
8. Forwards traffic from internal listeners to local services
9. Automatically cleans up mappings on graceful shutdown

## Configuration Files

### Server Configuration (wg-server.conf)
```ini
[Interface]
PrivateKey = <server_private_key>
Address = 10.0.0.1/24, fd00::1/64
ListenPort = 51820
MTU = 65280

[Peer]
PublicKey = <client_public_key>
AllowedIPs = 10.0.0.2/32, fd00::2/128
```

### Client Configuration (wg-client.conf)
```ini
[Interface]
PrivateKey = <client_private_key>
Address = 10.0.0.2/32, fd00::2/128
MTU = 65280

[Peer]
PublicKey = <server_public_key>
Endpoint = server.example.com:51820
AllowedIPs = 10.0.0.0/24, fd00::/64
PersistentKeepalive = 25
```

## API Endpoints

The server exposes a REST API within the WireGuard netstack:

### Port Mappings
- **POST** `/api/v1/port-mappings`
  - Create a new port mapping
  - Body: `{"local_addr": "127.0.0.1:8080", "remote_port": 8080, "client_ip": "10.0.0.2", "client_port": 12345}`

- **DELETE** `/api/v1/port-mappings?port=8080`
  - Remove a port mapping

### Heartbeat
- **POST** `/api/v1/heartbeat`
  - Send client heartbeat to maintain connection
  - Body: `{"client_ip": "10.0.0.2"}`
  - Server automatically removes mappings for clients that stop sending heartbeats (after 60 seconds)

## Flow Diagram

```
External Client -> Server:8080 -> WireGuard Tunnel -> Client:random_port -> localhost:8080
                                       ^
                                   Heartbeat every 20s
```

1. External client connects to server on port 8080
2. Server forwards to client's random internal port through WireGuard tunnel
3. Client forwards to local service (localhost:8080)
4. Client sends heartbeats every 20 seconds to maintain connection
5. Server checks client health every 30 seconds and removes mappings if client stops sending heartbeats for 60+ seconds

## Benefits

- **Simplified Configuration**: Server doesn't need port mapping flags
- **Dynamic Port Management**: Ports are opened/closed on demand
- **Automatic Cleanup**: Disconnected clients have their mappings automatically removed
- **Heartbeat Monitoring**: Real-time detection of client disconnections
- **Server Availability Check**: Client validates server connectivity before setup
- **Security**: Only WireGuard port is exposed externally
- **Scalability**: Easy to add/remove services without server restart
- **Clean Architecture**: Separated concerns with dedicated packages
- **Optimized I/O**: Buffer pool implementation for efficient connection copying
- **Configurable Performance**: Adjustable buffer sizes for different use cases
- **Memory Efficient**: Reusable buffer pools reduce garbage collection pressure
- **Graceful Shutdown**: Proper cleanup on client termination

## Example Usage

1. Start the server:
```bash
# Default configuration
./bin/rps -c wg-server.conf

# For high-traffic scenarios, use larger buffer size
./bin/rps -c wg-server.conf -b 256
```

2. Start the client to expose a local web server:
```bash
# Default configuration
./bin/rpc -c wg-client.conf -r localhost:8080-8080

# With matching buffer size for optimal performance
./bin/rpc -c wg-client.conf -b 256 -r localhost:8080-8080
```

3. Access the service externally:
```bash
curl http://server-ip:8080
```

The request will be tunneled through WireGuard to the client's localhost:8080 service.

## Performance Tuning

### Buffer Size Configuration

Both the server and client support configurable buffer sizes for I/O operations using the `-b` flag:

```bash
# Default buffer size (64KB) - good for most applications
./bin/rps -c wg-server.conf
./bin/rpc -c wg-client.conf -r localhost:8080-8080

# Small buffer (32KB) - saves memory for low-traffic services
./bin/rps -c wg-server.conf -b 32
./bin/rpc -c wg-client.conf -b 32 -r localhost:8080-8080

# Large buffer (256KB) - better performance for high-throughput applications
./bin/rps -c wg-server.conf -b 256
./bin/rpc -c wg-client.conf -b 256 -r localhost:8080-8080
```

### Buffer Size Recommendations

| Use Case | Recommended Buffer Size | Reasoning |
|----------|------------------------|-----------|
| Web services (HTTP/HTTPS) | 64KB (default) | Balanced performance and memory usage |
| File transfers (FTP, SCP) | 256KB - 512KB | Large sequential reads/writes benefit from bigger buffers |
| Game servers | 32KB - 64KB | Small, frequent packets don't need large buffers |
| Database connections | 64KB - 128KB | Mixed workload with moderate data sizes |
| Video streaming | 256KB - 1MB | High bandwidth with large continuous data |
| IoT/sensor data | 16KB - 32KB | Small, infrequent data transmissions |

### Memory vs Performance Trade-off

- **Smaller buffers (16-64KB)**: Use less memory, suitable for memory-constrained environments or many concurrent connections
- **Larger buffers (128KB+)**: Better throughput for high-bandwidth applications, but use more memory per connection

### Buffer Pool Implementation

The system uses an efficient buffer pool that:
- **Reuses buffers**: Reduces garbage collection pressure
- **Thread-safe**: Safe for concurrent use across multiple connections
- **Automatic cleanup**: Buffers are automatically returned to the pool after use
- **Zero-copy optimization**: Uses `io.CopyBuffer` for efficient data transfer
