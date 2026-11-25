package mcproto

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

var testConfig = Config{VersionName: "grus-stub", MOTD: "Grus native stub test", MaxPlayers: 20, LoginText: "This is a Grus stub server"}

// handshake builds the handshake packet (id 0x00) for the given next state.
func handshake(t *testing.T, protocol int32, next int32) []byte {
	t.Helper()
	data := AppendVarInt(nil, protocol)
	data = AppendString(data, "localhost")
	data = AppendUShort(data, 25565)
	data = AppendVarInt(data, next)
	return frame(t, 0x00, data)
}

func frame(t *testing.T, id int32, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WritePacket(&buf, id, data); err != nil {
		t.Fatalf("frame: %v", err)
	}
	return buf.Bytes()
}

func TestStatusExchangeOverPipe(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		err := Serve(server, testConfig)
		server.Close()
		done <- err
	}()

	const protocol = 774
	mustWrite(t, client, handshake(t, protocol, 1))
	mustWrite(t, client, frame(t, 0x00, nil)) // status request

	r := bufio.NewReader(client)
	resp, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		t.Fatalf("status response: %v", err)
	}
	if resp.ID != 0x00 {
		t.Fatalf("status response id = %d, want 0", resp.ID)
	}
	raw, err := NewDecoder(resp.Data).String(MaxStringBytes)
	if err != nil {
		t.Fatalf("status json string: %v", err)
	}
	var status struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int32  `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("status json: %v (%s)", err, raw)
	}
	if status.Version.Name != "grus-stub" || status.Version.Protocol != protocol {
		t.Errorf("version = %+v, want grus-stub and echoed protocol %d", status.Version, protocol)
	}
	if status.Players.Max != 20 || status.Players.Online != 0 {
		t.Errorf("players = %+v, want max 20 online 0", status.Players)
	}
	if status.Description.Text != "Grus native stub test" {
		t.Errorf("motd = %q", status.Description.Text)
	}

	const payload = int64(1234567890123)
	mustWrite(t, client, frame(t, 0x01, AppendLong(nil, payload)))
	pong, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		t.Fatalf("pong: %v", err)
	}
	if pong.ID != 0x01 {
		t.Fatalf("pong id = %d, want 1", pong.ID)
	}
	if got, _ := NewDecoder(pong.Data).Long(); got != payload {
		t.Errorf("pong payload = %d, want %d", got, payload)
	}

	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v after ping", err)
	}
}

func TestLoginRespondsWithReadableDisconnect(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		err := Serve(server, testConfig)
		server.Close()
		done <- err
	}()

	mustWrite(t, client, handshake(t, 774, 2))
	loginStart := AppendString(nil, "Player")
	loginStart = append(loginStart, make([]byte, 16)...) // UUID
	mustWrite(t, client, frame(t, 0x00, loginStart))

	pkt, err := ReadPacket(bufio.NewReader(client), MaxPacketLength)
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if pkt.ID != 0x00 {
		t.Fatalf("disconnect id = %d, want 0", pkt.ID)
	}
	text, err := NewDecoder(pkt.Data).String(MaxStringBytes)
	if err != nil {
		t.Fatalf("disconnect text: %v", err)
	}
	if !strings.Contains(text, "This is a Grus stub server") {
		t.Errorf("disconnect = %s, want the stub message", text)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned %v after login", err)
	}
}

func TestServeRejectsUnknownNextState(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() { done <- Serve(server, testConfig) }()

	mustWrite(t, client, handshake(t, 774, 3))
	if err := <-done; err == nil {
		t.Fatal("unknown next state accepted")
	}
}

func mustWrite(t *testing.T, conn net.Conn, b []byte) {
	t.Helper()
	if _, err := conn.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
}
