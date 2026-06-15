package org.sensorhub.impl.sensor.mavsdk.cs;

import org.sensorhub.impl.sensor.AbstractSensorControl;
import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;
import net.opengis.swe.v20.DataBlock;
import net.opengis.swe.v20.DataComponent;
import net.opengis.swe.v20.DataRecord;
import org.sensorhub.api.command.CommandException;
import io.mavsdk.System;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.vast.swe.SWEHelper;

public class CSGenericMavlinkHandler {
    private static final Logger log = LoggerFactory.getLogger(CSGenericMavlinkHandler.class);
    private final MavSdkDriver driver;
    private final System system;
    private ArmControl armControl;

    public CSGenericMavlinkHandler(MavSdkDriver driver, System system) {
        this.driver = driver;
        this.system = system;
    }

    public void start() {
        log.info("Starting CSGenericMavlinkHandler");
        armControl = new ArmControl(driver, system);
        driver.registerControl(armControl);
    }

    public void stop() {
        log.info("Stopping CSGenericMavlinkHandler");
    }

    private static class ArmControl extends AbstractSensorControl<MavSdkDriver> {
        private final System system;
        private final DataRecord commandDesc;

        public ArmControl(MavSdkDriver parent, System system) {
            super("Arm", parent);
            this.system = system;
            SWEHelper fac = new SWEHelper();
            commandDesc = fac.newDataRecord(4);
            commandDesc.setName("Arm");
            commandDesc.setDescription("Commands the vehicle to arm");
        }

        @Override
        public DataComponent getCommandDescription() {
            return commandDesc;
        }

        @Override
        protected boolean execCommand(DataBlock cmdData) throws CommandException {
            try {
                system.getAction().arm().blockingAwait();
                return true;
            } catch (Exception e) {
                getLogger().error("Arm command failed", e);
                throw new CommandException("Arm command failed", e);
            }
        }
    }
}
