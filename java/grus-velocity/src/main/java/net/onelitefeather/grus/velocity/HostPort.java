package net.onelitefeather.grus.velocity;

import java.util.Objects;

/**
 * A {@code host:port} address as the controller reports it, e.g. {@code 10.0.12.34:25565} or
 * {@code [2001:db8::1]:25565}.
 */
public record HostPort(String host, int port) {

    public HostPort {
        Objects.requireNonNull(host, "host");
        if (host.isBlank()) {
            throw new IllegalArgumentException("host must not be blank");
        }
        if (port < 1 || port > 65_535) {
            throw new IllegalArgumentException("port out of range: " + port);
        }
    }

    /**
     * Parses {@code host:port}. IPv6 literals must be bracketed.
     *
     * @throws IllegalArgumentException when the text is not a valid address
     */
    public static HostPort parse(String text) {
        if (text == null || text.isBlank()) {
            throw new IllegalArgumentException("address is empty");
        }
        String value = text.trim();
        String host;
        String portText;
        if (value.startsWith("[")) {
            int close = value.indexOf(']');
            if (close < 0 || close + 1 >= value.length() || value.charAt(close + 1) != ':') {
                throw new IllegalArgumentException("address must look like [host]:port: " + text);
            }
            host = value.substring(1, close);
            portText = value.substring(close + 2);
        } else {
            int colon = value.indexOf(':');
            if (colon < 0 || colon != value.lastIndexOf(':')) {
                throw new IllegalArgumentException("address must look like host:port: " + text);
            }
            host = value.substring(0, colon);
            portText = value.substring(colon + 1);
        }
        int port;
        try {
            port = Integer.parseInt(portText);
        } catch (NumberFormatException e) {
            throw new IllegalArgumentException("port is not a number: " + text, e);
        }
        return new HostPort(host, port);
    }
}
