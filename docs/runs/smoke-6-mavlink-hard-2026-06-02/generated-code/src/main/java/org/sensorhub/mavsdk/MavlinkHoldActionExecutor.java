package org.sensorhub.mavsdk;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.SocketTimeoutException;
import java.time.Duration;
import java.util.Objects;

final class MavlinkHoldActionExecutor implements MavsdkActionExecutor {
    private static final int MAVLINK_V1_MAGIC = 0xFE;
    private static final int COMMAND_LONG_MESSAGE_ID = 76;
    private static final int MAV_CMD_NAV_LOITER_UNLIM = 17;
    private static final int MAV_COMPONENT_AUTOPILOT1 = 1;
    private static final int COMMAND_ACK_MESSAGE_ID = 77;
    private static final int MAV_RESULT_ACCEPTED = 0;
    private static final int MAX_MAVLINK_PACKET_SIZE = 280;
    private static final int MAV_FRAME_GLOBAL = 0;
    private static final int PAYLOAD_LENGTH = 33;

    private final String host;
    private final int port;
    private final int sourceSystemId;
    private final int sourceComponentId;
    private final Duration acknowledgementTimeout;
    private final int maxAcknowledgementPackets;
    private int sequence;

    MavlinkHoldActionExecutor(String host, int port, int sourceSystemId, int sourceComponentId) {
        this(host, port, sourceSystemId, sourceComponentId, Duration.ofSeconds(5), 50);
    }

    MavlinkHoldActionExecutor(
            String host,
            int port,
            int sourceSystemId,
            int sourceComponentId,
            Duration acknowledgementTimeout,
            int maxAcknowledgementPackets) {
        this.host = validateHost(host);
        this.port = validatePort(port);
        this.sourceSystemId = validateUint8(sourceSystemId, "sourceSystemId");
        this.sourceComponentId = validateUint8(sourceComponentId, "sourceComponentId");
        this.acknowledgementTimeout = Objects.requireNonNull(acknowledgementTimeout, "acknowledgementTimeout must not be null");
        if (acknowledgementTimeout.isZero() || acknowledgementTimeout.isNegative()) {
            throw new IllegalArgumentException("acknowledgementTimeout must be positive");
        }
        if (maxAcknowledgementPackets < 1) {
            throw new IllegalArgumentException("maxAcknowledgementPackets must be positive");
        }
        this.maxAcknowledgementPackets = maxAcknowledgementPackets;
    }

    @Override
    public String executeHold(MavlinkSystemConnection connection, CommandRequest request) throws IOException {
        Objects.requireNonNull(connection, "connection must not be null");
        Objects.requireNonNull(request, "request must not be null");
        byte[] command = holdCommand(connection);
        InetAddress address = InetAddress.getByName(host);
        DatagramPacket packet = new DatagramPacket(command, command.length, address, port);
        try (DatagramSocket socket = new DatagramSocket()) {
            socket.setSoTimeout(socketTimeoutMillis());
            socket.send(packet);
            awaitAcceptedAcknowledgement(socket);
        }
        return "MAVLink hold command accepted by system " + connection.systemId();
    }

    private void awaitAcceptedAcknowledgement(DatagramSocket socket) throws IOException {
        byte[] buffer = new byte[MAX_MAVLINK_PACKET_SIZE];
        for (int attempt = 0; attempt < maxAcknowledgementPackets; attempt++) {
            DatagramPacket response = new DatagramPacket(buffer, buffer.length);
            try {
                socket.receive(response);
            } catch (SocketTimeoutException timeout) {
                throw new IOException("MAVLink hold command acknowledgement timed out", timeout);
            }
            if (isAcceptedHoldAcknowledgement(response.getData(), response.getLength())) {
                return;
            }
        }
        throw new IOException("MAVLink hold command acknowledgement was not accepted");
    }

    private static boolean isAcceptedHoldAcknowledgement(byte[] packet, int length) {
        if (packet == null || length < 11) {
            return false;
        }
        int magic = packet[0] & 0xFF;
        int payloadLength = packet[1] & 0xFF;
        if (magic == MAVLINK_V1_MAGIC) {
            return length >= 6 + payloadLength + 2
                && (packet[5] & 0xFF) == COMMAND_ACK_MESSAGE_ID
                && payloadLength >= 3
                && uint16(packet, 6) == MAV_CMD_NAV_LOITER_UNLIM
                && (packet[8] & 0xFF) == MAV_RESULT_ACCEPTED;
        }
        if (magic != 0xFD || length < 10 + payloadLength + 2) {
            return false;
        }
        int messageId = (packet[7] & 0xFF) | ((packet[8] & 0xFF) << 8) | ((packet[9] & 0xFF) << 16);
        return messageId == COMMAND_ACK_MESSAGE_ID
            && payloadLength >= 3
            && uint16(packet, 10) == MAV_CMD_NAV_LOITER_UNLIM
            && (packet[12] & 0xFF) == MAV_RESULT_ACCEPTED;
    }

    private static int uint16(byte[] packet, int offset) {
        return (packet[offset] & 0xFF) | ((packet[offset + 1] & 0xFF) << 8);
    }

    private byte[] holdCommand(MavlinkSystemConnection connection) {
        byte[] frame = new byte[PAYLOAD_LENGTH + 8];
        frame[0] = (byte) MAVLINK_V1_MAGIC;
        frame[1] = (byte) PAYLOAD_LENGTH;
        frame[2] = (byte) (sequence++ & 0xFF);
        frame[3] = (byte) sourceSystemId;
        frame[4] = (byte) sourceComponentId;
        frame[5] = (byte) COMMAND_LONG_MESSAGE_ID;
        int offset = 6;
        for (int index = 0; index < 7; index++) {
            offset = putFloat(frame, offset, 0.0f);
        }
        offset = putUInt16(frame, offset, MAV_CMD_NAV_LOITER_UNLIM);
        frame[offset++] = (byte) connection.systemId();
        frame[offset++] = (byte) MAV_COMPONENT_AUTOPILOT1;
        frame[offset++] = 0;
        frame[offset] = (byte) MAV_FRAME_GLOBAL;
        int crc = crcX25(frame, 1, PAYLOAD_LENGTH + 5);
        crc = accumulate((byte) 152, crc);
        putUInt16(frame, PAYLOAD_LENGTH + 6, crc);
        return frame;
    }

    private static int putFloat(byte[] frame, int offset, float value) {
        int bits = Float.floatToIntBits(value);
        frame[offset++] = (byte) bits;
        frame[offset++] = (byte) (bits >>> 8);
        frame[offset++] = (byte) (bits >>> 16);
        frame[offset++] = (byte) (bits >>> 24);
        return offset;
    }

    private static int putUInt16(byte[] frame, int offset, int value) {
        frame[offset++] = (byte) value;
        frame[offset++] = (byte) (value >>> 8);
        return offset;
    }

    private static int crcX25(byte[] bytes, int offset, int length) {
        int crc = 0xFFFF;
        for (int index = 0; index < length; index++) {
            crc = accumulate(bytes[offset + index], crc);
        }
        return crc;
    }

    private static int accumulate(byte value, int crc) {
        int tmp = (value & 0xFF) ^ (crc & 0xFF);
        tmp ^= (tmp << 4) & 0xFF;
        return ((crc >>> 8) ^ (tmp << 8) ^ (tmp << 3) ^ (tmp >>> 4)) & 0xFFFF;
    }

    private int socketTimeoutMillis() {
        long millis = acknowledgementTimeout.toMillis();
        return millis > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) millis;
    }

    private static String validateHost(String host) {
        if (host == null || host.isBlank()) {
            throw new IllegalArgumentException("host must not be blank");
        }
        return host;
    }

    private static int validatePort(int port) {
        if (port < 1 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
        return port;
    }

    private static int validateUint8(int value, String name) {
        if (value < 1 || value > 255) {
            throw new IllegalArgumentException(name + " must be in MAVLink uint8 range");
        }
        return value;
    }
}
