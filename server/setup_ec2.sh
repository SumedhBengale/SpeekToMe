#!/bin/bash
set -e

# Establish directories
sudo mkdir -p /var/www/speektome
sudo mkdir -p /var/www/speektome/uploads/meetings
sudo chown -R $USER:$USER /var/www/speektome

# Copy .env.example to .env if it does not exist
if [ ! -f /var/www/speektome/.env ]; then
  cp /var/www/speektome/.env.example /var/www/speektome/.env
  echo "Default .env created. Please configure your API keys in /var/www/speektome/.env"
fi

# Set up systemd service configuration file
sudo tee /etc/systemd/system/speektome.service > /dev/null <<EOF
[Unit]
Description=SpeekTo.Me Go Server
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=/var/www/speektome
ExecStart=/var/www/speektome/speektome-server
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
