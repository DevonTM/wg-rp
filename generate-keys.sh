#!/bin/bash

# Generate WireGuard keys for wg-rp setup

echo "Generating WireGuard keys..."

# Generate server keys
SERVER_PRIVATE=$(wg genkey)
SERVER_PUBLIC=$(echo "$SERVER_PRIVATE" | wg pubkey)

# Generate client keys  
CLIENT_PRIVATE=$(wg genkey)
CLIENT_PUBLIC=$(echo "$CLIENT_PRIVATE" | wg pubkey)

echo "Server Private Key: $SERVER_PRIVATE"
echo "Server Public Key:  $SERVER_PUBLIC"
echo ""
echo "Client Private Key: $CLIENT_PRIVATE" 
echo "Client Public Key:  $CLIENT_PUBLIC"
echo ""

# Create configured files
cat > wg-server.conf << EOF
[Interface]
PrivateKey = $SERVER_PRIVATE
Address = 10.0.0.1/24, fd00::1/64
ListenPort = 51820
MTU = 1420

[Peer]
PublicKey = $CLIENT_PUBLIC
AllowedIPs = 10.0.0.2/32, fd00::2/128
EOF

cat > wg-client.conf << EOF
[Interface]
PrivateKey = $CLIENT_PRIVATE
Address = 10.0.0.2/32, fd00::2/128
MTU = 1420

[Peer]
PublicKey = $SERVER_PUBLIC
Endpoint = YOUR_SERVER_IP:51820
AllowedIPs = 10.0.0.0/24, fd00::/64
PersistentKeepalive = 25
EOF

echo "Generated wg-server.conf and wg-client.conf"
echo "Don't forget to update YOUR_SERVER_IP in wg-client.conf with your actual server IP!"
