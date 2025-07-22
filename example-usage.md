# WG-RP Usage Examples

## Server (rps) Examples

### Example 1: Listen on all interfaces
```bash
# Listen on all interfaces port 8080, forward to WireGuard client 10.0.0.2:25565
./bin/rps -c wg-server.conf -a :8080-10.0.0.2:25565
```

### Example 2: Listen on specific IP
```bash
# Listen on 0.0.0.0:8080, forward to WireGuard client 10.0.0.2:25565
./bin/rps -c wg-server.conf -a 0.0.0.0:8080-10.0.0.2:25565
```

### Example 3: Multiple mappings
```bash
# Multiple services
./bin/rps -c wg-server.conf \
  -a 0.0.0.0:8080-10.0.0.2:25565 \
  -a :9090-10.0.0.2:8080 \
  -a 127.0.0.1:3000-10.0.0.2:3000
```

### Example 4: IPv6 support
```bash
# IPv6 addresses with bracket notation
./bin/rps -c wg-server.conf \
  -a [::]:8080-[fd00::2]:8080 \
  -a 0.0.0.0:9090-10.0.0.2:9090
```

## Client (rpc) Examples

### Example 1: Forward to local services
```bash
# Accept traffic from WireGuard on port 25565, forward to localhost:25565
./bin/rpc -c wg-client.conf -r localhost:25565-25565
```

### Example 2: Multiple local services
```bash
# Forward multiple ports to different local services
./bin/rpc -c wg-client.conf \
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

## Complete Setup Example

1. **Generate keys:**
   ```bash
   ./generate-keys.sh
   ```

2. **Update config files with the generated keys**

3. **Start the server (on public machine):**
   ```bash
   ./bin/rps -c wg-server.conf -a 0.0.0.0:8080-10.0.0.2:25565
   ```

4. **Start the client (on private machine behind NAT):**
   ```bash
   ./bin/rpc -c wg-client.conf -r localhost:25565-8080
   ```

5. **Test the connection:**
   ```bash
   # Connect to public_server_ip:8080, traffic will be forwarded to
   # the client's localhost:25565 through the WireGuard tunnel
   telnet public_server_ip 8080
   ```

## Flag Format Details

### Server (-a flag): `listen_ip:listen_port-target_ip:target_port`
- `listen_ip`: IP to listen on (empty = all interfaces)
- `listen_port`: Port to listen on publicly
- `target_ip`: WireGuard client IP to forward to (supports IPv6 with brackets)
- `target_port`: Port on WireGuard client to forward to
- Use "-" to separate listen and target parts to avoid IPv6 colon conflicts

### Client (-r flag): `target_ip:target_port-listen_port`
- `target_ip`: Local host to forward to (supports IPv6 with brackets)
- `target_port`: Local port to forward to
- `listen_port`: Port to listen on WireGuard interface
- Use "-" to separate target and listen parts to avoid IPv6 colon conflicts

## IPv6 Examples

### Server with IPv6:
```bash
# Listen on IPv6 and forward to IPv6 target
./bin/rps -c wg-server.conf -a [::]:8080-[fd00::2]:8080

# Mixed IPv4 listen, IPv6 target
./bin/rps -c wg-server.conf -a 0.0.0.0:8080-[fd00::2]:8080
```

### Client with IPv6:
```bash
# Forward to IPv6 local service
./bin/rpc -c wg-client.conf -r [::1]:8080-8080

# Forward to IPv4 local service
./bin/rpc -c wg-client.conf -r localhost:8080-8080
```
