package net.onelitefeather.grus.velocity;

/** The part of Velocity's server list that the Grus bookkeeping needs. Tests use a fake. */
public interface ProxyRegistry {

    /** Whether a server with this name is registered, from velocity.toml or from Grus. */
    boolean hasServer(String name);

    void registerServer(String name, HostPort address);

    void unregisterServer(String name);

    /** Pings a registered server once in the background and logs the result. Never throws. */
    void pingOnce(String name);
}
