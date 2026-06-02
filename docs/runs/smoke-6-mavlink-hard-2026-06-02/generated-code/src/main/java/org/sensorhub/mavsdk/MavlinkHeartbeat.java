package org.sensorhub.mavsdk;

import java.time.Instant;
import java.util.Arrays;
import java.util.Optional;

final class MavlinkHeartbeat {
    private static final int MAVLINK_V1_MAGIC = 0xFE;
    private static final int MAVLINK_V2_MAGIC = 0xFD;
    private static final int HEARTBEAT_MESSAGE_ID = 0;

    private MavlinkHeartbeat() {
    }

    static byte[] v1(int systemId, int componentId) {
        validateUint8(systemId, "systemId");
        validateUint8(componentId, "componentId");
        byte[] frame = new byte[17];
        frame[0] = (byte) MAVLINK_V1_MAGIC;
        frame[1] = 9;
        frame[2] = 0;
        frame[3] = (byte) systemId;
        frame[4] = (byte) componentId;
        frame[5] = HEARTBEAT_MESSAGE_ID;
        frame[6] = 0;
        frame[7] = 0;
        frame[8] = 0;
        frame[9] = 0;
        frame[10] = 2;
        frame[11] = 3;
        frame[12] = 0;
        frame[13] = 0;
        frame[14] = 3;
        return frame;
    }

    static Optional<MavlinkSystemConnection> parse(byte[] packet, int length) {
        if (packet == null || length < 1) {
            return Optional.empty();
        }
        int magic = unsigned(packet[0]);
        if (magic == MAVLINK_V1_MAGIC) {
            return parseV1(packet, length);
        }
        if (magic == MAVLINK_V2_MAGIC) {
            return parseV2(packet, length);
        }
        return Optional.empty();
    }

    private static Optional<MavlinkSystemConnection> parseV1(byte[] packet, int length) {
        if (length < 8) {
            return Optional.empty();
        }
        int payloadLength = unsigned(packet[1]);
        int frameLength = 6 + payloadLength + 2;
        if (length < frameLength || unsigned(packet[5]) != HEARTBEAT_MESSAGE_ID) {
            return Optional.empty();
        }
        if (payloadLength < 9) {
            return Optional.empty();
        }
        return Optional.of(new MavlinkSystemConnection(
            true,
            unsigned(packet[3]),
            unsigned(packet[4]),
            Arrays.copyOf(packet, frameLength),
            Instant.now()));
    }

    private static Optional<MavlinkSystemConnection> parseV2(byte[] packet, int length) {
        if (length < 12) {
            return Optional.empty();
        }
        int payloadLength = unsigned(packet[1]);
        int frameLength = 10 + payloadLength + 2;
        int messageId = unsigned(packet[7]) | (unsigned(packet[8]) << 8) | (unsigned(packet[9]) << 16);
        if (length < frameLength || messageId != HEARTBEAT_MESSAGE_ID) {
            return Optional.empty();
        }
        if (payloadLength < 9) {
            return Optional.empty();
        }
        return Optional.of(new MavlinkSystemConnection(
            true,
            unsigned(packet[5]),
            unsigned(packet[6]),
            Arrays.copyOf(packet, frameLength),
            Instant.now()));
    }

    private static int unsigned(byte value) {
        return value & 0xFF;
    }

    private static void validateUint8(int value, String name) {
        if (value < 0 || value > 255) {
            throw new IllegalArgumentException(name + " must be in MAVLink uint8 range");
        }
    }
}
