package org.sensorhub.impl.sensor.mavsdk.cs;

import io.mavsdk.System;
import io.mavsdk.action.Action;
import io.reactivex.Completable;
import org.junit.jupiter.api.Test;
import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;
import org.sensorhub.api.command.CommandException;

import static org.mockito.Mockito.*;
import static org.junit.jupiter.api.Assertions.*;

public class CSGenericMavlinkHandlerTest {

    @Test
    public void testStartRegistersArmControl() {
        MavSdkDriver mockDriver = mock(MavSdkDriver.class);
        System mockSystem = mock(System.class);

        CSGenericMavlinkHandler handler = new CSGenericMavlinkHandler(mockDriver, mockSystem);
        handler.start();

        verify(mockDriver, times(1)).registerControl(any());
    }

    @Test
    public void testArmCommandExecution() throws Exception {
        MavSdkDriver mockDriver = mock(MavSdkDriver.class);
        System mockSystem = mock(System.class);
        Action mockAction = mock(Action.class);

        when(mockSystem.getAction()).thenReturn(mockAction);
        when(mockAction.arm()).thenReturn(Completable.complete());

        CSGenericMavlinkHandler handler = new CSGenericMavlinkHandler(mockDriver, mockSystem);
        handler.start();

        org.mockito.ArgumentCaptor<org.sensorhub.api.command.IStreamingControlInterface> captor = org.mockito.ArgumentCaptor.forClass(org.sensorhub.api.command.IStreamingControlInterface.class);
        verify(mockDriver).registerControl(captor.capture());

        org.sensorhub.api.command.IStreamingControlInterface ctrl = captor.getValue();
        assertEquals("Arm", ctrl.getName());

        org.sensorhub.api.command.ICommandData mockCmdData = mock(org.sensorhub.api.command.ICommandData.class);
        when(mockCmdData.getID()).thenReturn(org.sensorhub.api.common.BigId.fromLong(0, 1L));
        
        java.util.concurrent.CompletableFuture<org.sensorhub.api.command.ICommandStatus> future = ctrl.submitCommand(mockCmdData);
        org.sensorhub.api.command.ICommandStatus status = future.get();

        verify(mockAction, times(1)).arm();
        assertEquals(org.sensorhub.api.command.ICommandStatus.CommandStatusCode.COMPLETED, status.getStatusCode());
    }
}
