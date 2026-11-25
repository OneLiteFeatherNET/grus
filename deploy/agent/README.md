# Grus agent

The agent runs Grus instances on a machine outside Kubernetes. It pulls its work from the controller over
HTTPS, so the machine needs no inbound path from the controller; proxies need to reach the instance ports.

## What it does

- Registers as a host and sends a heartbeat every 10 seconds.
- Long-polls `GET /v1/agents/{host}/desired`, starts missing instances as local processes, stops and kills on
  request and delivers console lines.
- Reports its instances and their log tails to `PUT /v1/agents/{host}/observed` every 5 seconds and after changes.
- Downloads template archives with its agent token and extracts them into the instance directory.
- Gives every instance `GRUS_URL` (the controller URL the agent uses), `GRUS_TOKEN`, `GRUS_INSTANCE`, `GRUS_POOL`,
  `GRUS_ROLE`, `GRUS_PORT`/`SERVER_PORT` and `GRUS_DIR`, plus the environment of the pool. Children never inherit
  the agent's own environment except PATH, HOME, LANG, LC_ALL, TZ, JAVA_HOME and TMPDIR.
- Allocates one port per instance from `--port-range` and reports `<address>:<port>`; `{{port}}` and `${GRUS_DIR}`
  in command arguments are replaced.

## Failure behaviour

- Controller or gateway down: instances keep running; the agent retries with backoff (1 s up to 30 s) and
  re-reports everything on reconnect. The controller keeps the state of the host until the agent is back.
- Controller restart: the agent re-reports its instances and the controller adopts them.
- Agent restart: **the instances of the agent are ended and started again** from the desired state (the agent
  cannot re-attach to child processes). Leftover processes are found through `grus.pid` files and killed on start.
- SIGTERM to the agent does not stop instances unless `--stop-on-exit` is set; with it they are stopped
  gracefully (stop command, SIGTERM, SIGKILL) before the agent exits.
- Dynamic instances lose their directory when they are removed; static ones keep it (worlds).

## Install (system service)

```sh
useradd --system --home /var/lib/grus-agent --create-home grus
install -m 0755 grus-agent /usr/local/bin/grus-agent
install -d -m 0750 -o grus /etc/grus-agent
# token: the plain secret; the controller only knows its SHA-256
install -m 0600 -o grus /dev/stdin /etc/grus-agent/token <<< "<agent token>"
cp deploy/agent/grus-agent.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now grus-agent
```

`/etc/grus-agent/agent.env`:

```sh
GRUS_URL=https://grus-dev-api.apps.onelite.feather
GRUS_AGENT_TOKEN_FILE=/etc/grus-agent/token
GRUS_AGENT_HOST=mc01
GRUS_AGENT_DIR=/var/lib/grus-agent/instances
GRUS_AGENT_ADDRESS=10.200.2.35
GRUS_AGENT_PORT_RANGE=30000-30100
GRUS_AGENT_JAVA=/home/cloud/.sdkman/candidates/java/25.0.3-tem/bin/java
# GRUS_AGENT_CA_FILE=/etc/grus-agent/ca.pem   # only when the system CAs do not trust the gateway
# GRUS_AGENT_STOP_ON_EXIT=true
```

## Install without root (user unit)

```sh
mkdir -p ~/grus-agent/bin ~/.config/grus-agent ~/.config/systemd/user
install -m 0755 grus-agent ~/grus-agent/bin/grus-agent
umask 077 && printf '%s' '<agent token>' > ~/.config/grus-agent/token
cp deploy/agent/grus-agent.user.service ~/.config/systemd/user/grus-agent.service
# write ~/.config/grus-agent/agent.env as above, with GRUS_AGENT_TOKEN_FILE=$HOME/.config/grus-agent/token
loginctl enable-linger "$USER"
systemctl --user daemon-reload && systemctl --user enable --now grus-agent
journalctl --user -u grus-agent -f
```

## Controller side

Generate a token and give the controller its hash:

```sh
TOKEN=$(openssl rand -hex 32)
printf '%s' "$TOKEN" | sha256sum | cut -d' ' -f1     # the hash
# GRUS_AGENT_TOKENS=mc01=<hash>[,other=<hash>]
```

The gateway in front of the controller must forward `/v1/agents`, `/v1/self`, `/v1/templates` and `/healthz`.
Check with `GET /v1/hosts` (admin token): the host shows `online: true`.

## Examples

`examples/agent/` holds an environment `minestom-jar`, a pool `mc01-lobby` (jvm, templates) and a native pool
`mc01-stub` that runs `./grus-stub-server` from a template.
