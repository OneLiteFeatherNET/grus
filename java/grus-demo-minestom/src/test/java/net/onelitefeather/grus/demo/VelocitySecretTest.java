package net.onelitefeather.grus.demo;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.junit.jupiter.api.Assertions.assertThrows;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.Function;
import org.junit.jupiter.api.Test;

class VelocitySecretTest {

    private static final String FILE_KEY = "VELOCITY_SECRET_FILE";

    @Test
    void returnsEmptyWhenNeitherVariableIsSet() {
        Optional<String> secret = DemoServer.velocitySecret(Map.of(), failingReader());

        assertFalse(secret.isPresent(), "no forwarding secret must be configured when no variable is set");
    }

    @Test
    void returnsEnvSecretWithoutReadingTheFile() {
        Map<String, String> env = Map.of("VELOCITY_SECRET", "from-env", FILE_KEY, "/run/secret");

        Optional<String> secret = DemoServer.velocitySecret(env, failingReader());

        assertEquals(Optional.of("from-env"), secret, "an env secret must win over the file");
    }

    @Test
    void readsSecretFromFileWhenEnvSecretIsAbsent() {
        Map<String, String> env = Map.of(FILE_KEY, "secrets/velocity.txt");

        Optional<String> secret = DemoServer.velocitySecret(env, readerReturning("secrets/velocity.txt", "from-file"));

        assertEquals(Optional.of("from-file"), secret, "the file content must be used as the secret");
    }

    @Test
    void readsSecretFromFileWhenEnvSecretIsEmpty() {
        Map<String, String> env = Map.of("VELOCITY_SECRET", "", FILE_KEY, "secrets/velocity.txt");

        Optional<String> secret = DemoServer.velocitySecret(env, readerReturning("secrets/velocity.txt", "from-file"));

        assertEquals(Optional.of("from-file"), secret, "an empty env secret must not block the file");
    }

    @Test
    void trimsTrailingNewlineFromFileSecret() {
        Map<String, String> env = Map.of(FILE_KEY, "velocity.txt");

        Optional<String> secret = DemoServer.velocitySecret(env, readerReturning("velocity.txt", "  s3cret\n"));

        assertEquals(Optional.of("s3cret"), secret, "surrounding whitespace of the file must be stripped");
    }

    @Test
    void resolvesFileSecretPathExactlyAsGiven() {
        List<Path> requested = new ArrayList<>();
        Function<Path, String> reader = path -> {
            requested.add(path);
            return "secret";
        };

        DemoServer.velocitySecret(Map.of(FILE_KEY, "config/velocity.txt"), reader);

        assertEquals(List.of(Path.of("config/velocity.txt")), requested,
                "a relative path must be handed to the reader unchanged so it resolves against the working directory");
    }

    @Test
    void failsWhenRequestedFileCannotBeRead() {
        Map<String, String> env = Map.of(FILE_KEY, "missing/velocity.txt");
        Function<Path, String> reader = path -> {
            throw new IllegalStateException("no such file");
        };

        IllegalStateException error = assertThrows(IllegalStateException.class,
                () -> DemoServer.velocitySecret(env, reader));

        assertTrue(error.getMessage().contains("missing/velocity.txt"),
                "the error must name the file so the operator can fix the mount or path");
    }

    @Test
    void failsWhenRequestedFileIsBlank() {
        Map<String, String> env = Map.of(FILE_KEY, "velocity.txt");

        IllegalStateException error = assertThrows(IllegalStateException.class,
                () -> DemoServer.velocitySecret(env, readerReturning("velocity.txt", "  \n")));

        assertTrue(error.getMessage().contains("empty"),
                "a blank secret file must not silently start the server without forwarding");
    }

    @Test
    void ignoresBlankFileVariableWhenNoSecretIsSet() {
        Map<String, String> env = new HashMap<>();
        env.put(FILE_KEY, "");

        Optional<String> secret = DemoServer.velocitySecret(env, failingReader());

        assertEquals(Optional.empty(), secret, "an empty file variable counts as unset");
    }

    private static Function<Path, String> failingReader() {
        return path -> {
            throw new AssertionError("the file must not be read: " + path);
        };
    }

    private static Function<Path, String> readerReturning(String expectedPath, String content) {
        return path -> {
            assertEquals(Path.of(expectedPath), path, "unexpected file requested");
            return content;
        };
    }
}
