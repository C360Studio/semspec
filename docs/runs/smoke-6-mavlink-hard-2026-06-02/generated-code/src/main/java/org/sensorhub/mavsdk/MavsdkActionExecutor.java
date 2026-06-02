package org.sensorhub.mavsdk;

import java.io.IOException;

@FunctionalInterface
interface MavsdkActionExecutor {
    String executeHold(MavlinkSystemConnection connection, CommandRequest request) throws IOException, InterruptedException;
}
