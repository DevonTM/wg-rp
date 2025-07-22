# WireGuard-based Reverse Proxy

This project implements a reverse proxy system using WireGuard tunnels.

## Components

### RPS (Reverse Proxy Server)
- Accepts public traffic and forwards it through WireGuard to clients
- Usage: `./rps -c wg-server.conf -a :25565-10.0.0.2:25565 -a :80-10.0.0.2:80`

### RPC (Reverse Proxy Client)  
- Receives traffic from WireGuard server and forwards to local services
- Usage: `./rpc -c wg-client.conf -r localhost:25565-25565 -r localhost:80-80`

## Setup

1. **Generate WireGuard keys:**
   ```bash
   wg genkey | tee server_private.key | wg pubkey > server_public.key
   wg genkey | tee client_private.key | wg pubkey > client_public.key
   ```

2. **Configure WireGuard files:**
   - Edit `wg-server.conf` with your server keys and network settings
   - Edit `wg-client.conf` with your client keys and server endpoint

3. **Build the applications:**
   ```bash
   go build -o rps ./cmd/rps
   go build -o rpc ./cmd/rpc
   ```

4. **Run the server (on public server):**
   ```bash
   sudo ./rps -c wg-server.conf -a :25565-10.0.0.2:25565
   ```

5. **Run the client (on local machine):**
   ```bash
   sudo ./rpc -c wg-client.conf -r localhost:25565-25565
   ```

## Flags

### RPS Flags:
- `-c`: WireGuard configuration file (default: wg.conf)
- `-v`: Enable verbose logging
- `-a`: Proxy mapping in format `listen_ip:listen_port-target_ip:target_port` (can be used multiple times)
  - Example: `-a :25565-10.0.0.2:25565` listens on port 25565 and forwards to WireGuard client at 10.0.0.2:25565
  - Example: `-a 0.0.0.0:8080-[fd00::2]:8080` supports IPv6 addresses

### RPC Flags:
- `-c`: WireGuard configuration file (default: wg.conf)
- `-v`: Enable verbose logging
- `-r`: Route mapping in format `target_ip:target_port-listen_port` (can be used multiple times)
  - Example: `-r localhost:25565-25565` forwards traffic from WireGuard port 25565 to localhost:25565
  - Example: `-r [::1]:8080-8080` supports IPv6 addresses

## Example Usage

To expose a local web server (port 80) and Minecraft server (port 25565):

**On the public server:**
```bash
sudo ./rps -c wg-server.conf -a :80-10.0.0.2:80 -a :25565-10.0.0.2:25565
```

**On the local machine:**
```bash
sudo ./rpc -c wg-client.conf -r localhost:80-80 -r localhost:25565-25565
```

This will make your local services accessible through the public server's IP address.

## Notes

- Both applications require root privileges to create WireGuard interfaces
- Make sure ports used in `-a` and `-r` flags match between RPS and RPC
- The new format uses "-" to separate listen and target parts, avoiding conflicts with IPv6 colons
- IPv6 addresses are properly supported using standard bracket notation
- The WireGuard tunnel must be properly configured with matching keys and allowed IPs
