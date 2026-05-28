package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Evidence anchors: mavlink.raw-mavlink-direct, HEARTBEAT

func TestHeartbeatService(t *testing.T) {
	// Reset global state
	mu.Lock()
	lastHeartbeat = nil
	mu.Unlock()

	// 1. Verify 404 behavior when no heartbeat is present
	req := httptest.NewRequest(http.MethodGet, "/heartbeat", nil)
	rr := httptest.NewRecorder()
	handleHeartbeat(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("expected 404, got %v", status)
	}

	// Start UDP server
	node, err := startMavlinkListener("127.0.0.1:14541")
	if err != nil {
		t.Fatalf("failed to start MAVLink listener: %v", err)
	}
	defer node.Close()

	// Connect UDP client
	conn, err := net.Dial("udp", "127.0.0.1:14541")
	if err != nil {
		t.Fatalf("failed to dial UDP: %v", err)
	}
	defer conn.Close()

	// Read mock data
	hb1, err := os.ReadFile("testdata/heartbeat1.bin")
	if err != nil {
		t.Fatalf("failed to read testdata/heartbeat1.bin: %v", err)
	}
	hb2, err := os.ReadFile("testdata/heartbeat2.bin")
	if err != nil {
		t.Fatalf("failed to read testdata/heartbeat2.bin: %v", err)
	}

	// 2. Send first heartbeat
	if _, err := conn.Write(hb1); err != nil {
		t.Fatalf("failed to send heartbeat1: %v", err)
	}

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Verify 200 behavior
	req = httptest.NewRequest(http.MethodGet, "/heartbeat", nil)
	rr = httptest.NewRecorder()
	handleHeartbeat(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("expected 200, got %v", status)
	}

	var resp HeartbeatResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.SystemID != 10 || resp.ComponentID != 20 || resp.AutopilotType != 2 || resp.BaseMode != 3 {
		t.Errorf("unexpected fields for hb1: %+v", resp)
	}

	// 3. Send second heartbeat to verify update
	if _, err := conn.Write(hb2); err != nil {
		t.Fatalf("failed to send heartbeat2: %v", err)
	}

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Verify updated behavior
	req = httptest.NewRequest(http.MethodGet, "/heartbeat", nil)
	rr = httptest.NewRecorder()
	handleHeartbeat(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("expected 200, got %v", status)
	}

	var resp2 HeartbeatResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp2.SystemID != 15 || resp2.ComponentID != 25 || resp2.AutopilotType != 7 || resp2.BaseMode != 8 {
		t.Errorf("unexpected fields for hb2: %+v", resp2)
	}
}