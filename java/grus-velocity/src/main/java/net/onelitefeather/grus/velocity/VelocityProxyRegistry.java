package net.onelitefeather.grus.velocity;

import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.ServerInfo;

import java.net.InetSocketAddress;

/** {@link ProxyRegistry} backed by Velocity's live server list. */
final class VelocityProxyRegistry implements ProxyRegistry {

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
}
