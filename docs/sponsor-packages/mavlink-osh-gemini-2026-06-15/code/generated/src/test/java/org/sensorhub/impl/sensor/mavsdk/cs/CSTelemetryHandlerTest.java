package org.sensorhub.impl.sensor.mavsdk.cs;

import io.mavsdk.System;
import io.mavsdk.telemetry.Telemetry;
import io.reactivex.Flowable;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;
import org.sensorhub.impl.sensor.DefaultLocationOutput;
import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;

import static org.mockito.Mockito.*;

public class CSTelemetryHandlerTest {

    @Test
    public void testStartSubscribesToPosition() {
        MavSdkDriver mockDriver = mock(MavSdkDriver.class);
        System mockSystem = mock(System.class);
        Telemetry mockTelemetry = mock(Telemetry.class);
        DefaultLocationOutput mockLocationOutput = mock(DefaultLocationOutput.class);

        when(mockDriver.getLocationOutput()).thenReturn(mockLocationOutput);
        when(mockSystem.getTelemetry()).thenReturn(mockTelemetry);
        
        Telemetry.Position mockPos = new Telemetry.Position(47.39, 8.54, 488.0f, 488.0f);
        when(mockTelemetry.getPosition()).thenReturn(Flowable.just(mockPos));

        CSTelemetryHandler handler = new CSTelemetryHandler(mockDriver, mockSystem);
        handler.start();

        // The handler should have subscribed and updated the location output
        verify(mockLocationOutput, timeout(1000).times(1)).updateLocation(
                anyDouble(),
                eq(8.54),
                eq(47.39),
                eq(488.0),
                eq(false)
        );
        
        handler.stop();
    }
}
