package org.sensorhub.impl.sensor.mavsdk.cs;

import io.mavsdk.System;
import io.mavsdk.action.Action;
import io.reactivex.Completable;
import org.junit.jupiter.api.Test;
import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;
import org.sensorhub.api.command.CommandException;

import static org.mockito.Mockito.*;
import static org.junit.jupiter.api.Assertions.*;

public class CSControlHandlerTest {

    @Test
    public void testStartRegistersTakeoffControl() {
        MavSdkDriver mockDriver = mock(MavSdkDriver.class);
        System mockSystem = mock(System.class);

        CSControlHandler handler = new CSControlHandler(mockDriver, mockSystem);
        handler.start();

        verify(mockDriver, times(1)).registerControl(any());
    }

    @Test
    public void testTakeoffCommandExecution() throws Exception {
        MavSdkDriver mockDriver = mock(MavSdkDriver.class);
        System mockSystem = mock(System.class);
        Action mockAction = mock(Action.class);

        when(mockSystem.getAction()).thenReturn(mockAction);
        when(mockAction.takeoff()).thenReturn(Completable.complete());

        CSControlHandler handler = new CSControlHandler(mockDriver, mockSystem);
        handler.start();

        // How to get the registered control? Let's capture it.
        org.mockito.ArgumentCaptor<org.sensorhub.api.command.IStreamingControlInterface> captor = org.mockito.ArgumentCaptor.forClass(org.sensorhub.api.command.IStreamingControlInterface.class);
        verify(mockDriver).registerControl(captor.capture());

        org.sensorhub.api.command.IStreamingControlInterface ctrl = captor.getValue();
        assertEquals("Takeoff", ctrl.getName());

        // We can execute it via submitCommand if we mock the DataBlock
        org.sensorhub.api.command.ICommandData mockCmdData = mock(org.sensorhub.api.command.ICommandData.class);
        when(mockCmdData.getID()).thenReturn(org.sensorhub.api.common.BigId.fromLong(0, 1L));
        
        java.util.concurrent.CompletableFuture<org.sensorhub.api.command.ICommandStatus> future = ctrl.submitCommand(mockCmdData);
        org.sensorhub.api.command.ICommandStatus status = future.get();

        verify(mockAction, times(1)).takeoff();
        assertEquals(org.sensorhub.api.command.ICommandStatus.CommandStatusCode.COMPLETED, status.getStatusCode());
    }
}
