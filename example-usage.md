# WG-RP Usage Examples

## Server (rps) Examples

### Example 1: Listen on all interfaces
```bash
# Listen on all interfaces port 25565, forward to WireGuard client 10.0.0.2:25565
./bin/rps -c wg-server.conf -a :25565:10.0.0.2
```

### Example 2: Listen on specific IP
```bash
# Listen on 0.0.0.0:8080, forward to WireGuard client 10.0.0.2:8080
./bin/rps -c wg-server.conf -a 0.0.0.0:8080:10.0.0.2
```

### Example 3: Multiple mappings
```bash
# Multiple services
./bin/rps -c wg-server.conf \
  -a 0.0.0.0:25565:10.0.0.2 \
  -a :8080:10.0.0.2 \
  -a 127.0.0.1:3000:10.0.0.2
```

## Client (rpc) Examples

### Example 1: Forward to local services
```bash
# Accept traffic from WireGuard on port 25565, forward to localhost:25565
./bin/rpc -c wg-client.conf -r localhost:25565
```

### Example 2: Multiple local services
```bash
# Forward multiple ports to different local services
./bin/rpc -c wg-client.conf \
  -r localhost:25565 \
  -r localhost:8080 \
  -r 192.168.1.100:3000
```

## Complete Setup Example

1. **Generate keys:**
   ```bash
   ./generate-keys.sh
   ```

2. **Update config files with the generated keys**

3. **Start the server (on public machine):**
   ```bash
   ./bin/rps -c wg-server.conf -a 0.0.0.0:25565:10.0.0.2
   ```

4. **Start the client (on private machine behind NAT):**
   ```bash
   ./bin/rpc -c wg-client.conf -r localhost:25565
   ```

5. **Test the connection:**
   ```bash
   # Connect to public_server_ip:25565, traffic will be forwarded to
   # the client's localhost:25565 through the WireGuard tunnel
   telnet public_server_ip 25565
   ```

## Flag Format Details

### Server (-a flag): `listen_ip:listen_port:target_ip`
- `listen_ip`: IP to listen on (empty = all interfaces)
- `listen_port`: Port to listen on publicly
- `target_ip`: WireGuard client IP to forward to
- Target port is same as listen port

### Client (-r flag): `host:port`
- `host`: Local host to forward to
- `port`: Local port to forward to (must match server's listen_port)
