# WG-RP Usage Examples

## Server (rps) Examples

The server no longer requires port mapping flags. It automatically handles port mappings requested by clients.

### Example 1: Basic server startup
```bash
# Start server with default config
./bin/rps

# Start server with custom config
./bin/rps -c wg-server.conf

# Start server with verbose logging
./bin/rps -c wg-server.conf -v

# Start server with custom buffer size (32KB for I/O operations)
./bin/rps -c wg-server.conf -b 32

# Show version
./bin/rps -V
```

The server will:
- Read WireGuard configuration
- Start WireGuard netstack
- Listen for client connections and API requests
- Automatically open/close ports as requested by clients
- Monitor client health via heartbeats
- Clean up ports when clients disconnect

## Client (rpc) Examples

### Example 1: Forward to local services
```bash
# Expose localhost:25565 to server port 25565
./bin/rpc -c wg-client.conf -r localhost:25565-25565

# With custom buffer size (32KB for I/O operations)
./bin/rpc -c wg-client.conf -b 32 -r localhost:25565-25565

# Show version
./bin/rpc -V
```

### Example 2: Multiple local services
```bash
# Forward multiple ports to different local services
./bin/rpc -c wg-client.conf \
  -r localhost:25565-25565 \
  -r localhost:8080-8080 \
  -r 192.168.1.100:3000-3000

# With custom buffer size for better performance on high-traffic services
./bin/rpc -c wg-client.conf -b 128 \
  -r localhost:25565-25565 \
  -r localhost:8080-8080 \
  -r 192.168.1.100:3000-3000
```

### Example 3: IPv6 support
```bash
# IPv6 addresses with bracket notation
./bin/rpc -c wg-client.conf \
  -r [::1]:8080-8080 \
  -r localhost:9090-9090
```

### Example 4: Verbose logging and buffer tuning
```bash
# Enable verbose logging to see connection details with custom buffer size
./bin/rpc -c wg-client.conf -v -b 32 -r localhost:8080-8080

# For high-throughput applications, use larger buffer
./bin/rpc -c wg-client.conf -v -b 256 -r localhost:8080-8080
```

The client will:
- Check server availability before starting
- Register port mappings with the server
- Start heartbeat mechanism (every 30 seconds)
- Handle graceful shutdown with cleanup

## Complete Setup Example

1. **Generate keys:**
   ```bash
   ./generate-keys.sh
   ```

2. **Update config files with the generated keys**

3. **Start the server (on public machine):**
   ```bash
   ./bin/rps -c wg-server.conf
   
   # For high-traffic scenarios, use larger buffer size
   ./bin/rps -c wg-server.conf -b 128
   ```

4. **Start the client (on private machine behind NAT):**
   ```bash
   ./bin/rpc -c wg-client.conf -r localhost:25565-8080
   
   # With matching buffer size for optimal performance
   ./bin/rpc -c wg-client.conf -b 128 -r localhost:25565-8080
   ```

5. **Test the connection:**
   ```bash
   # Connect to public_server_ip:8080, traffic will be forwarded to
   # the client's localhost:25565 through the WireGuard tunnel
   telnet public_server_ip 8080
   ```

The client will automatically:
- Check if server is available
- Register port 8080 with the server
- Start sending heartbeats to maintain the connection
- Clean up the mapping when it shuts down

## Flag Format Details

### Server Flags
The server automatically handles port mappings requested by clients:
- `-c config_file`: WireGuard configuration file (default: wg-server.conf)
- `-v`: Enable verbose logging on WireGuard device
- `-b buffer_size`: Buffer size for I/O operations in KB (default: 64, minimum: 1)
- `-V`: Show version and exit

### Client Flags
- `-c config_file`: WireGuard configuration file (default: wg-client.conf)
- `-v`: Enable verbose logging on WireGuard device
- `-b buffer_size`: Buffer size for I/O operations in KB (default: 64, minimum: 1)
- `-r local_ip:local_port-remote_port`: Route mapping (can be used multiple times)
- `-V`: Show version and exit

### Client (-r flag): `local_ip:local_port-remote_port`
- `local_ip`: Local host to forward to (supports IPv6 with brackets)
- `local_port`: Local port to forward to
- `remote_port`: Port to expose on server
- Use "-" to separate local and remote parts to avoid IPv6 colon conflicts

Example: `-r localhost:8080-8080` means:
- Server will listen on port 8080
- Traffic will be forwarded to client's localhost:8080

### Buffer Size Optimization (-b flag)
The buffer size controls the I/O buffer used for connection copying operations:
- **Default**: 64KB (good balance for most applications)
- **Small files/low traffic**: 32KB or less (saves memory)
- **Large files/high throughput**: 128KB, 256KB, or higher (better performance)
- **Minimum**: 1KB (enforced limit)
- **Format**: Specify in KB (e.g., `-b 128` for 128KB buffer)

Buffer size recommendations:
- **Web services**: 64KB (default)
- **File transfers**: 256KB or higher
- **Game servers**: 32-64KB
- **Database connections**: 64-128KB
- **Video streaming**: 256KB or higher

## IPv6 Examples

### Server with IPv6:
```bash
# Start server (automatically handles both IPv4 and IPv6)
./bin/rps -c wg-server.conf

# With custom buffer size for high-traffic IPv6 services
./bin/rps -c wg-server.conf -b 128
```

### Client with IPv6:
```bash
# Forward to IPv6 local service
./bin/rpc -c wg-client.conf -r [::1]:8080-8080

# Forward to IPv4 local service
./bin/rpc -c wg-client.conf -r localhost:8080-8080

# Mixed examples with custom buffer size
./bin/rpc -c wg-client.conf -b 128 \
  -r [::1]:8080-8080 \
  -r localhost:3000-3000 \
  -r 192.168.1.100:9090-9090

# For video streaming over IPv6, use larger buffer
./bin/rpc -c wg-client.conf -b 256 \
  -r [::1]:8080-8080
```

## Connection Monitoring

The system includes automatic connection monitoring:

### Heartbeat Mechanism
- Client sends heartbeats every 20 seconds
- Server checks client health every 30 seconds  
- Server considers client dead after 60 seconds without heartbeat
- All port mappings for dead clients are automatically removed

### Graceful Shutdown
- Press Ctrl+C on client to gracefully shutdown
- Client automatically removes all port mappings from server
- Server immediately closes associated listening ports

### Server Availability Check
- Client checks server availability before starting
- Fails fast if server is unreachable
- Ensures reliable connection before port mapping setup
