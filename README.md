# WireGuard-based Reverse Proxy

This project implements a reverse proxy system using WireGuard tunnels.

## Components

### RPS (Reverse Proxy Server)
- Accepts public traffic and forwards it through WireGuard to clients
- Usage: `./rps -c wg-server.conf -a :25565:10.0.0.2 -a :80:10.0.0.2`

### RPC (Reverse Proxy Client)  
- Receives traffic from WireGuard server and forwards to local services
- Usage: `./rpc -c wg-client.conf -r localhost:25565 -r localhost:80`

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
   sudo ./rps -c wg-server.conf -a :25565:10.0.0.2
   ```

5. **Run the client (on local machine):**
   ```bash
   sudo ./rpc -c wg-client.conf -r localhost:25565
   ```

## Flags

### RPS Flags:
- `-c`: WireGuard configuration file (default: wg.conf)
- `-a`: Proxy mapping in format `:port:target_ip` (can be used multiple times)
  - Example: `-a :25565:10.0.0.2` listens on port 25565 and forwards to WireGuard client at 10.0.0.2

### RPC Flags:
- `-c`: WireGuard configuration file (default: wg.conf)
- `-r`: Route mapping in format `host:port` (can be used multiple times)
  - Example: `-r localhost:25565` forwards traffic from WireGuard to localhost:25565

## Example Usage

To expose a local web server (port 80) and Minecraft server (port 25565):

**On the public server:**
```bash
sudo ./rps -c wg-server.conf -a :80:10.0.0.2 -a :25565:10.0.0.2
```

**On the local machine:**
```bash
sudo ./rpc -c wg-client.conf -r localhost:80 -r localhost:25565
```

This will make your local services accessible through the public server's IP address.

## Notes

- Both applications require root privileges to create WireGuard interfaces
- Make sure ports used in `-a` and `-r` flags match between RPS and RPC
- The WireGuard tunnel must be properly configured with matching keys and allowed IPs
