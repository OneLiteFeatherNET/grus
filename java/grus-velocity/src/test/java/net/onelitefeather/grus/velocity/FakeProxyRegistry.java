package net.onelitefeather.grus.velocity;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/** In-memory server list that records every change. Each test creates its own instance. */
final class FakeProxyRegistry implements ProxyRegistry {

    private final Map<String, HostPort> servers = new HashMap<>();
    private final List<String> calls = new ArrayList<>();

    /** A server that exists before Grus touches the list, like one from velocity.toml. */
    FakeProxyRegistry withStaticServer(String name, HostPort address) {
        servers.put(name, address);
        return this;
    }

    @Override
    public boolean hasServer(String name) {
        return servers.containsKey(name);
    }

    @Override
    public void registerServer(String name, HostPort address) {
        servers.put(name, address);
        calls.add("register " + name);
    }

    @Override
    public void unregisterServer(String name) {
        servers.remove(name);
        calls.add("unregister " + name);
    }

    List<String> calls() {
        return List.copyOf(calls);
    }

    HostPort address(String name) {
        return servers.get(name);
    }
}
