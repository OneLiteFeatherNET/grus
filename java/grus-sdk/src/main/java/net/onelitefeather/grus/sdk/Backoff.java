package net.onelitefeather.grus.sdk;

import java.util.concurrent.ThreadLocalRandom;

/**
 * Waiting strategy between reconnect attempts of {@link EventStream}. Tests inject an immediate
 * implementation instead of sleeping.
 */
@FunctionalInterface
public interface Backoff {

    /**
     * Blocks before reconnect attempt number {@code attempt} (0 for the first retry).
     */
    void sleep(int attempt) throws InterruptedException;

    /** Production default: 1 s doubling up to 30 s, with jitter between half and the full delay. */
    static Backoff defaults() {
        return attempt -> Thread.sleep(delayMillis(attempt));
    }

    /** Delay in milliseconds for {@code attempt}: between half of and the full capped exponential value. */
    static long delayMillis(int attempt) {
        long base = Math.min(30_000L, 1_000L << Math.min(Math.max(attempt, 0), 5));
        long half = base / 2;
        return half + ThreadLocalRandom.current().nextLong(half + 1);
    }
}
