package org.sensorhub.impl.sensor.mavsdk;

import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.attribute.PosixFilePermission;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class MavSdkServerHandler {
    private static final Logger log = LoggerFactory.getLogger(MavSdkServerHandler.class);
    private String connectionString;
    private Process serverProcess;
    private int grpcPort = 50051;
    private String customBinaryPath;

    public MavSdkServerHandler(String connectionString) {
        this.connectionString = connectionString;
    }
    
    public String getConnectionString() {
        return connectionString;
    }
    
    public void setCustomBinaryPath(String path) {
        this.customBinaryPath = path;
    }

    public void start() throws Exception {
        String binaryPath = customBinaryPath != null ? customBinaryPath : extractBinary();
        ProcessBuilder pb = new ProcessBuilder(binaryPath, "-p", String.valueOf(grpcPort), connectionString);
        pb.redirectErrorStream(true);
        serverProcess = pb.start();
        
        // Wait a bit for it to start
        boolean started = false;
        long timeout = System.currentTimeMillis() + 5000;
        while (System.currentTimeMillis() < timeout) {
            if (!serverProcess.isAlive()) {
                break;
            }
            Thread.sleep(100);
            started = true;
        }
        
        if (!started || !serverProcess.isAlive()) {
            throw new Exception("mavsdk_server failed to start");
        }
    }

    public void stop() {
        if (serverProcess != null && serverProcess.isAlive()) {
            serverProcess.destroy();
            try {
                serverProcess.waitFor(2, TimeUnit.SECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            if (serverProcess.isAlive()) {
                serverProcess.destroyForcibly();
            }
        }
    }

    private String extractBinary() throws IOException {
        String os = System.getProperty("os.name").toLowerCase();
        String arch = System.getProperty("os.arch").toLowerCase();
        String binName;
        
        if (os.contains("win")) {
            binName = arch.contains("aarch64") ? "mavsdk_server_win_arm64.exe" : "mavsdk_server_win_x64.exe";
        } else if (os.contains("mac")) {
            binName = arch.contains("aarch64") ? "mavsdk_server_macos_arm64" : "mavsdk_server_macos_x64";
        } else {
            binName = arch.contains("aarch64") ? "mavsdk_server_linux-arm64-musl" : "mavsdk_server_musl_x86_64";
        }

        Path tempFile = Files.createTempFile("mavsdk_server", "");
        try (InputStream is = getClass().getResourceAsStream("/natives/" + binName)) {
            if (is == null) {
                throw new IOException("Could not find mavsdk_server binary in resources: /natives/" + binName);
            }
            Files.copy(is, tempFile, java.nio.file.StandardCopyOption.REPLACE_EXISTING);
        }
        
        Files.setPosixFilePermissions(tempFile, Set.of(
            PosixFilePermission.OWNER_READ, PosixFilePermission.OWNER_WRITE, PosixFilePermission.OWNER_EXECUTE
        ));
        
        tempFile.toFile().deleteOnExit();
        return tempFile.toAbsolutePath().toString();
    }
}
