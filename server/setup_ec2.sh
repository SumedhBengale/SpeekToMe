#!/bin/bash
set -e

# Establish directories
mkdir -p /home/ec2-user/speektome
mkdir -p /home/ec2-user/speektome/uploads/meetings

# Copy .env.example to .env if it does not exist
if [ ! -f /home/ec2-user/speektome/.env ]; then
  cp /home/ec2-user/speektome/.env.example /home/ec2-user/speektome/.env
  echo "Default .env created. Please configure your API keys in /home/ec2-user/speektome/.env"
fi

# Set up systemd service configuration file
sudo tee /etc/systemd/system/speektome.service > /dev/null <<EOF
[Unit]
Description=SpeekTo.Me Go Server
After=network.target

[Service]
Type=simple
User=ec2-user
WorkingDirectory=/home/ec2-user/speektome
ExecStart=/home/ec2-user/speektome/speektome-server
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# Reload daemon and enable on boot
sudo systemctl daemon-reload
sudo systemctl enable speektome
echo "Systemd service speektome configured successfully!"
