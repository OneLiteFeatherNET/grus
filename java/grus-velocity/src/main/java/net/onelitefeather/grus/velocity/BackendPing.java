package net.onelitefeather.grus.velocity;

import java.util.concurrent.CompletionException;

/** Log lines for the one-off ping of a backend server after it was registered. */
final class BackendPing {

    private BackendPing() {
    }

    static String pinged(String name, String address, String versionName, int online, int max) {
        return "pinged " + name + " at " + address + ": " + versionName + " " + online + "/" + max;
    }

    static String failed(String name, Throwable error) {
        Throwable cause = error instanceof CompletionException && error.getCause() != null ? error.getCause() : error;
        String reason = cause.getMessage() == null ? cause.getClass().getName() : cause.getMessage();
        return "ping " + name + " failed: " + reason;
    }
}
