package org.sensorhub.mavsdk;

import java.nio.file.Path;
import java.time.Duration;
import java.util.Objects;
import java.util.OptionalInt;
import java.util.concurrent.TimeUnit;

final class ManagedMavsdkServer implements AutoCloseable {
    private static final Duration TERMINATION_TIMEOUT = Duration.ofSeconds(2);

    private final Path executable;
    private final ServerProcess process;

    ManagedMavsdkServer(Path executable, Process process) {
        this(executable, new JvmServerProcess(process));
    }

    ManagedMavsdkServer(Path executable, ServerProcess process) {
        this.executable = Objects.requireNonNull(executable, "executable must not be null");
        this.process = Objects.requireNonNull(process, "process must not be null");
    }

    Path executable() {
        return executable;
    }

    boolean isAlive() {
        return process.isAlive();
    }

    OptionalInt exitCode() {
        return process.exitCode();
    }

    @Override
    public void close() {
        process.close();
    }

    interface ServerProcess extends AutoCloseable {
        boolean isAlive();

        OptionalInt exitCode();

        @Override
        void close();
    }

    private static final class JvmServerProcess implements ServerProcess {
        private final Process process;

        private JvmServerProcess(Process process) {
            this.process = Objects.requireNonNull(process, "process must not be null");
        }

        @Override
        public boolean isAlive() {
            return process.isAlive();
        }

        @Override
        public OptionalInt exitCode() {
            try {
                return OptionalInt.of(process.exitValue());
            } catch (IllegalThreadStateException running) {
                return OptionalInt.empty();
            }
        }

        @Override
        public void close() {
            if (!process.isAlive()) {
                return;
            }
            process.destroy();
            try {
                boolean exited = process.waitFor(TERMINATION_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
                if (!exited) {
                    process.destroyForcibly();
                    process.waitFor(TERMINATION_TIMEOUT.toMillis(), TimeUnit.MILLISECONDS);
                }
            } catch (InterruptedException error) {
                Thread.currentThread().interrupt();
                process.destroyForcibly();
            }
        }
    }
}
