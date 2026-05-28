//go:build integration || e2e

package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/minimal"
)

func TestE2E_GCSFlow(t *testing.T) {
	// Reset state
	mu.Lock()
	lastHeartbeat = nil
	mu.Unlock()

	// Start services like main()
	node, err := startMavlinkListener("127.0.0.1:14540")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer node.Close()

	server := startHTTPServer("127.0.0.1:8080")
	defer server.Close()

	// Wait for listener and server
	time.Sleep(200 * time.Millisecond)

	// Step 1: Send MAVLink HEARTBEAT to UDP port 14540
	clientNode, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints: []gomavlib.EndpointConf{
			gomavlib.EndpointUDPClient{Address: "127.0.0.1:14540"},
		},
		Dialect:     minimal.Dialect,
		OutVersion:  gomavlib.V2,
		OutSystemID: 100,
		OutComponentID: 200,
	})
	if err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer clientNode.Close()

	time.Sleep(100 * time.Millisecond)

	err = clientNode.WriteMessageAll(&minimal.MessageHeartbeat{
		Type:           minimal.MAV_TYPE_QUADROTOR,
		Autopilot:      minimal.MAV_AUTOPILOT_GENERIC,
		BaseMode:       minimal.MAV_MODE_FLAG_SAFETY_ARMED,
		CustomMode:     0,
		SystemStatus:   minimal.MAV_STATE_ACTIVE,
		MavlinkVersion: 3,
	})
	if err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	// Give it a moment to process the UDP packet
	time.Sleep(100 * time.Millisecond)

	// Step 2: GET /heartbeat
	resp, err := http.Get("http://127.0.0.1:8080/heartbeat")
	if err != nil {
		t.Fatalf("failed to GET /heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var hb HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&hb); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if hb.SystemID != 100 {
		t.Errorf("got system_id %d, want 100", hb.SystemID)
	}
	if hb.ComponentID != 200 {
		t.Errorf("got component_id %d, want 200", hb.ComponentID)
	}
	if hb.AutopilotType != int(minimal.MAV_AUTOPILOT_GENERIC) {
		t.Errorf("got autopilot_type %d, want %d", hb.AutopilotType, minimal.MAV_AUTOPILOT_GENERIC)
	}
	if hb.BaseMode != int(minimal.MAV_MODE_FLAG_SAFETY_ARMED) {
		t.Errorf("got base_mode %d, want %d", hb.BaseMode, minimal.MAV_MODE_FLAG_SAFETY_ARMED)
	}
	if hb.ReceivedAt.IsZero() {
		t.Errorf("received_at is zero")
	}
}