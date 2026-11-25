package net.onelitefeather.grus.velocity;

import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.ServerInfo;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.net.InetSocketAddress;

/** {@link ProxyRegistry} backed by Velocity's live server list. */
final class VelocityProxyRegistry implements ProxyRegistry {

    private static final Logger LOG = LoggerFactory.getLogger(VelocityProxyRegistry.class);

    private final ProxyServer proxy;

    VelocityProxyRegistry(ProxyServer proxy) {
        this.proxy = proxy;
    }

    @Override
    public boolean hasServer(String name) {
        return proxy.getServer(name).isPresent();
    }

    @Override
    public void registerServer(String name, HostPort address) {
        proxy.registerServer(new ServerInfo(name, new InetSocketAddress(address.host(), address.port())));
    }

    @Override
    public void unregisterServer(String name) {
        proxy.getServer(name).ifPresent(server -> proxy.unregisterServer(server.getServerInfo()));
    }

    @Override
    public void pingOnce(String name) {
        proxy.getServer(name).ifPresent(server -> server.ping().whenComplete((ping, error) -> {
            if (error != null) {
                LOG.info(BackendPing.failed(name, error));
                return;
            }
            int online = ping.getPlayers().map(players -> players.getOnline()).orElse(0);
            int max = ping.getPlayers().map(players -> players.getMax()).orElse(0);
            String version = ping.getVersion() == null ? "unknown" : ping.getVersion().getName();
            LOG.info(BackendPing.pinged(name, server.getServerInfo().getAddress().toString(), version, online, max));
        }));
    }
}
