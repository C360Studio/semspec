package org.sensorhub.mavsdk;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketTimeoutException;
import java.time.Duration;
import java.util.Objects;
import java.util.Optional;

final class MavlinkSystemConnector {
    private static final int MAX_MAVLINK_PACKET_SIZE = 280;

    private final String host;
    private final int port;
    private final Duration receiveTimeout;
    private final int maxAttempts;

    MavlinkSystemConnector(String host, int port, Duration receiveTimeout, int maxAttempts) {
        this.host = requireHost(host);
        if (port < 1 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
        this.port = port;
        this.receiveTimeout = Objects.requireNonNull(receiveTimeout, "receiveTimeout must not be null");
        if (receiveTimeout.isZero() || receiveTimeout.isNegative()) {
            throw new IllegalArgumentException("receiveTimeout must be positive");
        }
        if (maxAttempts < 1) {
            throw new IllegalArgumentException("maxAttempts must be positive");
        }
        this.maxAttempts = maxAttempts;
    }

    MavlinkSystemConnection awaitConnectedVehicle() throws IOException {
        return tryAwaitConnectedVehicle()
            .orElseThrow(() -> new IOException("MAVLink HEARTBEAT was not observed before connection timeout"));
    }

    Optional<MavlinkSystemConnection> tryAwaitConnectedVehicle() throws IOException {
        InetAddress address = InetAddress.getByName(host);
        try (DatagramSocket socket = new DatagramSocket(null)) {
            socket.setReuseAddress(true);
            socket.bind(new InetSocketAddress(address, port));
            socket.setSoTimeout(socketTimeoutMillis());
            return receiveHeartbeat(socket);
        }
    }

    private Optional<MavlinkSystemConnection> receiveHeartbeat(DatagramSocket socket) throws IOException {
        byte[] buffer = new byte[MAX_MAVLINK_PACKET_SIZE];
        for (int attempt = 0; attempt < maxAttempts; attempt++) {
            DatagramPacket packet = new DatagramPacket(buffer, buffer.length);
            Optional<MavlinkSystemConnection> connection = receivePacket(socket, packet);
            if (connection.isPresent()) {
                return connection;
            }
        }
        return Optional.empty();
    }

    private Optional<MavlinkSystemConnection> receivePacket(DatagramSocket socket, DatagramPacket packet) throws IOException {
        try {
            socket.receive(packet);
            return MavlinkHeartbeat.parse(packet.getData(), packet.getLength());
        } catch (SocketTimeoutException timeout) {
            return Optional.empty();
        }
    }

    private int socketTimeoutMillis() {
        long millis = receiveTimeout.toMillis();
        return millis > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) millis;
    }

    private static String requireHost(String host) {
        if (host == null || host.isBlank()) {
            throw new IllegalArgumentException("host must not be blank");
        }
        return host;
    }
}
