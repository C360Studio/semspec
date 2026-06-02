package org.sensorhub.mavsdk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.time.Duration;
import java.time.Instant;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;

class MavsdkActionControlTest {
    @Test
    void unsupportedActionReturnsFailedCommandResultWithoutCallingExecutor() {
        // Requirement eng-test-coverage: unsupported control actions must have a named FAILED branch test.
        MavlinkSystemConnection connection = connectedVehicle();
        AtomicBoolean called = new AtomicBoolean(false);
        MavsdkActionControl control = new MavsdkActionControl(connection, (vehicle, request) -> {
            called.set(true);
            return "unexpected";
        });

        CommandResult result = control.executeAction(new CommandRequest("cmd-land", "LAND"));

        assertEquals("cmd-land", result.commandId());
        assertEquals(CommandStatus.FAILED, result.status());
        assertEquals("Unsupported MAVSDK action", result.message());
        assertFalse(called.get(), "unsupported action must not call the MAVSDK action executor");
    }

    @Test
    void constructorRejectsNullActionExecutor() {
        NullPointerException error = assertThrows(
            NullPointerException.class,
            () -> new MavsdkActionControl(connectedVehicle(), null));

        assertTrue(error.getMessage().contains("actionExecutor"));
    }

    @Test
    void executeActionRejectsNullRequest() {
        MavsdkActionControl control = new MavsdkActionControl(connectedVehicle(), (vehicle, request) -> "unused");

        NullPointerException error = assertThrows(NullPointerException.class, () -> control.executeAction(null));

        assertTrue(error.getMessage().contains("request"));
    }

    @Test
    void executorIOExceptionReturnsFailedCommandResult() {
        MavsdkActionControl control = new MavsdkActionControl(connectedVehicle(), (vehicle, request) -> {
            throw new IOException("transport unavailable");
        });

        CommandResult result = control.executeAction(CommandRequest.holdPosition("cmd-hold"));

        assertEquals(CommandStatus.FAILED, result.status());
        assertEquals("MAVSDK action failed", result.message());
    }

    @Test
    void mavlinkHoldActionExecutorSendsCommandLongAndRequiresAcceptedAcknowledgement() throws Exception {
        // Requirement sitl-telemetry-and-control: control path sends a real MAVLink command and waits for COMMAND_ACK.
        MavlinkSystemConnection connection = connectedVehicle();
        try (DatagramSocket receiver = new DatagramSocket(0, InetAddress.getLoopbackAddress())) {
            byte[] received = new byte[280];
            DatagramPacket commandPacket = new DatagramPacket(received, received.length);
            Thread autopilot = new Thread(() -> receiveHoldCommandAndSendAck(receiver, commandPacket), "mavlink-ack-sender");
            autopilot.start();
            MavlinkHoldActionExecutor executor = new MavlinkHoldActionExecutor(
                InetAddress.getLoopbackAddress().getHostAddress(),
                receiver.getLocalPort(),
                250,
                1,
                Duration.ofSeconds(2),
                3);

            String message = executor.executeHold(connection, CommandRequest.holdPosition("cmd-hold"));

            autopilot.join(1_000);
            assertEquals("MAVLink hold command accepted by system 42", message);
            assertEquals(0xFE, received[0] & 0xFF, "control path should send a MAVLink v1 COMMAND_LONG frame");
            assertEquals(76, received[5] & 0xFF, "COMMAND_LONG message id must be used for hold");
            assertEquals(17, uint16(received, 34), "MAV_CMD_NAV_LOITER_UNLIM must be sent for HOLD");
            assertEquals(42, received[36] & 0xFF, "target system must come from the connected vehicle");
        }
    }

    @Test
    void mavlinkHoldActionExecutorReportsFailedActionWhenAckIsMissing() throws Exception {
        try (DatagramSocket receiver = new DatagramSocket(0, InetAddress.getLoopbackAddress())) {
            MavsdkActionControl control = new MavsdkActionControl(
                connectedVehicle(),
                new MavlinkHoldActionExecutor(
                    InetAddress.getLoopbackAddress().getHostAddress(),
                    receiver.getLocalPort(),
                    250,
                    1,
                    Duration.ofMillis(50),
                    1));

            CommandResult result = control.executeAction(CommandRequest.holdPosition("cmd-hold"));

            assertEquals(CommandStatus.FAILED, result.status());
            assertEquals("MAVSDK action failed", result.message());
        }
    }

    @Test
    void commandRequestConstructorValidatesRequiredText() {
        assertThrows(NullPointerException.class, () -> new CommandRequest(null, "HOLD"));
        assertThrows(IllegalArgumentException.class, () -> new CommandRequest(" ", "HOLD"));
        assertThrows(NullPointerException.class, () -> new CommandRequest("cmd", null));
        assertThrows(IllegalArgumentException.class, () -> new CommandRequest("cmd", " "));
        assertEquals("HOLD", new CommandRequest("cmd", "hold").actionName());
    }

    @Test
    void commandResultConstructorValidatesRequiredFields() {
        Instant now = Instant.now();
        assertThrows(NullPointerException.class, () -> new CommandResult(null, CommandStatus.SUCCEEDED, "ok", now));
        assertThrows(IllegalArgumentException.class, () -> new CommandResult(" ", CommandStatus.SUCCEEDED, "ok", now));
        assertThrows(NullPointerException.class, () -> new CommandResult("cmd", null, "ok", now));
        assertThrows(NullPointerException.class, () -> new CommandResult("cmd", CommandStatus.SUCCEEDED, null, now));
        assertThrows(IllegalArgumentException.class, () -> new CommandResult("cmd", CommandStatus.SUCCEEDED, " ", now));
        assertThrows(NullPointerException.class, () -> new CommandResult("cmd", CommandStatus.SUCCEEDED, "ok", null));
    }

    @Test
    void ogcDataStreamObservationConstructorValidatesRequiredFields() {
        Instant now = Instant.now();
        Map<String, Object> result = Map.of("mavsdk_core_connected", true);
        assertThrows(NullPointerException.class, () -> new OgcDataStreamObservation(null, "HEARTBEAT", now, result));
        assertThrows(IllegalArgumentException.class, () -> new OgcDataStreamObservation(" ", "HEARTBEAT", now, result));
        assertThrows(NullPointerException.class, () -> new OgcDataStreamObservation("stream", null, now, result));
        assertThrows(IllegalArgumentException.class, () -> new OgcDataStreamObservation("stream", " ", now, result));
        assertThrows(NullPointerException.class, () -> new OgcDataStreamObservation("stream", "HEARTBEAT", null, result));
        assertThrows(NullPointerException.class, () -> new OgcDataStreamObservation("stream", "HEARTBEAT", now, null));
    }

    @Test
    void mapperRejectsNullConnectionInput() {
        NullPointerException error = assertThrows(
            NullPointerException.class,
            () -> OgcDataStreamMapper.fromHeartbeat("px4-heartbeat", null));

        assertTrue(error.getMessage().contains("connection"));
    }

    private static void receiveHoldCommandAndSendAck(DatagramSocket receiver, DatagramPacket commandPacket) {
        try {
            receiver.receive(commandPacket);
            byte[] ack = commandAck();
            DatagramPacket ackPacket = new DatagramPacket(
                ack,
                ack.length,
                commandPacket.getAddress(),
                commandPacket.getPort());
            receiver.send(ackPacket);
        } catch (IOException error) {
            throw new IllegalStateException("failed to acknowledge test hold command", error);
        }
    }

    private static byte[] commandAck() {
        return new byte[] {
            (byte) 0xFE, 3, 0, 1, 1, 77,
            17, 0, 0,
            0, 0
        };
    }

    private static int uint16(byte[] packet, int offset) {
        return (packet[offset] & 0xFF) | ((packet[offset + 1] & 0xFF) << 8);
    }

    private static MavlinkSystemConnection connectedVehicle() {
        return MavlinkHeartbeat.parse(MavlinkHeartbeat.v1(42, 1), 17).orElseThrow();
    }
}
