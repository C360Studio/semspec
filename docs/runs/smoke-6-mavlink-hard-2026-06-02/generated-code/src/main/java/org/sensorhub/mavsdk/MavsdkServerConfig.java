package org.sensorhub.mavsdk;

import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Objects;

record MavsdkServerConfig(
        Path executable,
        List<String> arguments,
        Duration readinessPollInterval,
        Duration startupTimeout,
        int maxRestartAttempts) {
    MavsdkServerConfig(Path executable, List<String> arguments, Duration readinessPollInterval, Duration startupTimeout) {
        this(executable, arguments, readinessPollInterval, startupTimeout, 0);
    }

    MavsdkServerConfig {
        Objects.requireNonNull(executable, "executable must not be null");
        Objects.requireNonNull(arguments, "arguments must not be null");
        Objects.requireNonNull(readinessPollInterval, "readinessPollInterval must not be null");
        Objects.requireNonNull(startupTimeout, "startupTimeout must not be null");
        if (!Files.isRegularFile(executable) || !Files.isExecutable(executable)) {
            throw new IllegalArgumentException("mavsdk_server executable must be an executable file");
        }
        if (readinessPollInterval.isZero() || readinessPollInterval.isNegative()) {
            throw new IllegalArgumentException("readinessPollInterval must be positive");
        }
        if (startupTimeout.isZero() || startupTimeout.isNegative()) {
            throw new IllegalArgumentException("startupTimeout must be positive");
        }
        if (maxRestartAttempts < 0) {
            throw new IllegalArgumentException("maxRestartAttempts must not be negative");
        }
        arguments = List.copyOf(arguments);
    }
}
