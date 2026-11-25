package mcproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Config is what the stub advertises in the server list and answers to a login attempt.
type Config struct {
	VersionName string
	MOTD        string
	MaxPlayers  int
	LoginText   string
}

const (
	handshakePacket = 0x00
	nextStatus      = 1
	nextLogin       = 2

	statusRequestPacket = 0x00
	statusResponsePkt   = 0x00
	pingPacket          = 0x01
	loginStartPacket    = 0x00
	loginDisconnectPkt  = 0x00
)

type statusResponse struct {
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

// Serve speaks one client connection to completion: the handshake, then either the server-list
// exchange (status request and ping) or a login that is refused with Config.LoginText. It returns nil
// when the exchange ends normally and an error for protocol violations or I/O failures. The caller owns
// rw and must set deadlines and close it.
func Serve(rw io.ReadWriter, cfg Config) error {
	r := bufio.NewReader(rw)
	hs, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if hs.ID != handshakePacket {
		return fmt.Errorf("handshake: unexpected packet id %d", hs.ID)
	}
	d := NewDecoder(hs.Data)
	protocol, err := d.VarInt()
	if err != nil {
		return fmt.Errorf("handshake protocol: %w", err)
	}
	if _, err := d.String(255); err != nil {
		return fmt.Errorf("handshake address: %w", err)
	}
	if _, err := d.UShort(); err != nil {
		return fmt.Errorf("handshake port: %w", err)
	}
	next, err := d.VarInt()
	if err != nil {
		return fmt.Errorf("handshake next state: %w", err)
	}
	switch next {
	case nextStatus:
		return serveStatus(rw, r, protocol, cfg)
	case nextLogin:
		return serveLogin(rw, r, cfg)
	default:
		return fmt.Errorf("handshake: unsupported next state %d", next)
	}
}

func serveStatus(w io.Writer, r *bufio.Reader, protocol int32, cfg Config) error {
	req, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		return fmt.Errorf("read status request: %w", err)
	}
	if req.ID != statusRequestPacket {
		return fmt.Errorf("status: unexpected packet id %d", req.ID)
	}
	body, err := statusJSON(protocol, cfg)
	if err != nil {
		return err
	}
	if err := WritePacket(w, statusResponsePkt, AppendString(nil, body)); err != nil {
		return fmt.Errorf("write status response: %w", err)
	}

	ping, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		// Clients that only wanted the list may leave without pinging.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("read ping: %w", err)
	}
	if ping.ID != pingPacket {
		return fmt.Errorf("status: unexpected packet id %d", ping.ID)
	}
	payload, err := NewDecoder(ping.Data).Long()
	if err != nil {
		return fmt.Errorf("ping payload: %w", err)
	}
	if err := WritePacket(w, pingPacket, AppendLong(nil, payload)); err != nil {
		return fmt.Errorf("write pong: %w", err)
	}
	return nil
}

func serveLogin(w io.Writer, r *bufio.Reader, cfg Config) error {
	start, err := ReadPacket(r, MaxPacketLength)
	if err != nil {
		return fmt.Errorf("read login start: %w", err)
	}
	if start.ID != loginStartPacket {
		return fmt.Errorf("login: unexpected packet id %d", start.ID)
	}
	reason, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: cfg.LoginText})
	if err != nil {
		return fmt.Errorf("encode disconnect: %w", err)
	}
	if err := WritePacket(w, loginDisconnectPkt, AppendString(nil, string(reason))); err != nil {
		return fmt.Errorf("write disconnect: %w", err)
	}
	return nil
}

func statusJSON(protocol int32, cfg Config) (string, error) {
	var resp statusResponse
	resp.Version.Name = cfg.VersionName
	resp.Version.Protocol = protocol
	resp.Players.Max = cfg.MaxPlayers
	resp.Players.Online = 0
	resp.Description.Text = cfg.MOTD
	b, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("encode status: %w", err)
	}
	return string(b), nil
}
