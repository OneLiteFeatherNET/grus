package net.onelitefeather.grus.sdk;

/**
 * Thrown when a Grus API call fails: a non-2xx response ({@link #status()} is the HTTP status)
 * or a transport error ({@link #status()} is 0).
 */
public final class GrusException extends RuntimeException {

    private final int status;
    private final String body;

    public GrusException(String message, int status, String body) {
        this(message, status, body, null);
    }

    public GrusException(String message, int status, String body, Throwable cause) {
        super(message, cause);
        this.status = status;
        this.body = body == null ? "" : body;
    }

    public int status() {
        return status;
    }

    /** Response body of the failed call, empty for transport errors. */
    public String body() {
        return body;
    }
}
