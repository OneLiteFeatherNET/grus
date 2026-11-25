package net.onelitefeather.grus.demo;

import net.kyori.adventure.text.Component;
import net.minestom.server.Auth;
import net.minestom.server.MinecraftServer;
import net.minestom.server.entity.GameMode;
import net.minestom.server.coordinate.Pos;
import net.minestom.server.command.builder.Command;
import net.minestom.server.event.player.AsyncPlayerConfigurationEvent;
import net.minestom.server.event.player.PlayerSpawnEvent;
import net.minestom.server.instance.InstanceContainer;
import net.minestom.server.instance.block.Block;
import net.onelitefeather.grus.minestom.GrusMinestom;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;
import java.util.Optional;
import java.util.function.Function;

/**
 * Minimal lobby for the Grus prototype: a flat world, Velocity modern forwarding when a secret is
 * configured, a {@code /hub} command, and the Grus integration.
 *
 * <p>Environment: {@code GRUS_PORT} (default 25565), {@code GRUS_INSTANCE} (shown in the welcome
 * message), {@code VELOCITY_SECRET} (optional forwarding secret), {@code VELOCITY_SECRET_FILE}
 * (optional path, relative to the working directory, to a file holding the secret; used when
 * {@code VELOCITY_SECRET} is empty and fails startup if the file cannot be read), plus the
 * {@code GRUS_*} variables read by {@link GrusMinestom}.
 */
public final class DemoServer {

    private static final int SPAWN_Y = 42;

    private DemoServer() {
    }

    public static void main(String[] args) {
        Optional<String> velocitySecret = velocitySecret(System.getenv(), DemoServer::readSecretFile);
        MinecraftServer server = velocitySecret.isPresent()
                ? MinecraftServer.init(new Auth.Velocity(velocitySecret.get()))
                : MinecraftServer.init();

        // SIGTERM (docker stop, kubelet) runs the shutdown hook, which saves and disconnects players.
        Runtime.getRuntime().addShutdownHook(new Thread(MinecraftServer::stopCleanly, "grus-demo-shutdown"));

        String instance = System.getenv().getOrDefault("GRUS_INSTANCE", "local");
        InstanceContainer lobby = MinecraftServer.getInstanceManager().createInstanceContainer();
        lobby.setGenerator(unit -> {
            unit.modifier().fillHeight(0, 1, Block.BEDROCK);
            unit.modifier().fillHeight(1, 40, Block.DIRT);
            unit.modifier().fillHeight(40, 41, Block.GRASS_BLOCK);
        });

        MinecraftServer.getGlobalEventHandler()
                .addListener(AsyncPlayerConfigurationEvent.class, event -> {
                    event.setSpawningInstance(lobby);
                    event.getPlayer().setRespawnPoint(new Pos(0.5, SPAWN_Y, 0.5));
                })
                .addListener(PlayerSpawnEvent.class, event -> {
                    if (event.isFirstSpawn()) {
                        event.getPlayer().setGameMode(GameMode.ADVENTURE);
                        event.getPlayer().sendMessage(Component.text("Grus demo lobby " + instance));
                    }
                });

        Command hub = new Command("hub");
        hub.setDefaultExecutor((sender, context) -> sender.sendMessage(Component.text("you are already in a lobby")));
        MinecraftServer.getCommandManager().register(hub);

        int port = parsePort(System.getenv("GRUS_PORT"));
        server.start("0.0.0.0", port);

        GrusMinestom.install().ifPresent(GrusMinestom::ready);
    }

    /**
     * Resolves the Velocity forwarding secret. {@code VELOCITY_SECRET} wins when set and non-empty;
     * otherwise a set {@code VELOCITY_SECRET_FILE} is read through {@code readFile} and stripped.
     * A requested file that cannot be read or is blank is an error, so the server never starts
     * without forwarding when a secret was asked for.
     */
    static Optional<String> velocitySecret(Map<String, String> env, Function<Path, String> readFile) {
        String secret = env.get("VELOCITY_SECRET");
        if (secret != null && !secret.isEmpty()) {
            return Optional.of(secret);
        }
        String file = env.get("VELOCITY_SECRET_FILE");
        if (file == null || file.isEmpty()) {
            return Optional.empty();
        }
        Path path = Path.of(file);
        String content;
        try {
            content = readFile.apply(path);
        } catch (RuntimeException e) {
            throw new IllegalStateException("VELOCITY_SECRET_FILE is set but cannot be read: " + path, e);
        }
        String stripped = content == null ? "" : content.strip();
        if (stripped.isEmpty()) {
            throw new IllegalStateException("VELOCITY_SECRET_FILE is empty: " + path);
        }
        return Optional.of(stripped);
    }

    private static String readSecretFile(Path path) {
        try {
            return Files.readString(path, StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    private static int parsePort(String raw) {
        if (raw == null || raw.isBlank()) {
            return 25565;
        }
        return Integer.parseInt(raw.trim());
    }
}
