#!/bin/sh
# Prepares /data (the proxy working directory) and starts Velocity.
set -eu

cd /data

# Templates may mount their own velocity.toml; the image default is used only when there is none.
if [ ! -f velocity.toml ]; then
    cp /opt/velocity/velocity.toml velocity.toml
fi

mkdir -p plugins
cp /opt/velocity/plugins/*.jar plugins/

# The forwarding secret comes from the environment (secretEnv of the pool) and is only readable by the proxy.
if [ -n "${VELOCITY_SECRET:-}" ]; then
    (
        umask 077
        printf '%s' "$VELOCITY_SECRET" > forwarding.secret
    )
fi

exec java -XX:MaxRAMPercentage=75 -jar /opt/velocity/velocity.jar
