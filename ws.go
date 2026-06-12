package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	opcodeText    = 0x1
	opcodeClose   = 0x8
	opcodePing    = 0x9
	opcodePong    = 0xA
	wsWriteWindow = 5 * time.Second
)

type slideMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type wsClient struct {
	conn net.Conn
	mu   sync.Mutex
}

var (
	wsClients     = make(map[*wsClient]struct{})
	wsClientsLock sync.Mutex
)

func slideshowWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
		return
	}

	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		http.Error(w, "Missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket unsupported", http.StatusInternalServerError)
		return
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		log.Printf("WebSocket hijack failed: %v", err)
		return
	}

	accept := computeWebSocketAccept(key)
	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
		conn.Close()
		return
	}
	if _, err := rw.WriteString("Upgrade: websocket\r\n"); err != nil {
		conn.Close()
		return
	}
	if _, err := rw.WriteString("Connection: Upgrade\r\n"); err != nil {
		conn.Close()
		return
	}
	if _, err := rw.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n\r\n"); err != nil {
		conn.Close()
		return
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return
	}

	client := &wsClient{conn: conn}
	registerWSClient(client)
	defer unregisterWSClient(client)
	defer conn.Close()

	log.Printf("WebSocket client connected from %s", conn.RemoteAddr().String())

	if current, err := getCurrentImageData(); err == nil {
		if err := sendSlideToClient(client, current); err != nil {
			log.Printf("Initial WebSocket slide send failed: %v", err)
			return
		}
	}

	if weather, ok := getCurrentWeatherData(); ok {
		if err := sendWeatherToClient(client, weather); err != nil {
			log.Printf("Initial WebSocket weather send failed: %v", err)
			return
		}
	}

	if err := consumeWebSocketFrames(client, rw.Reader); err != nil {
		log.Printf("WebSocket client disconnected: %v", err)
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !hasHeaderToken(r.Header.Get("Connection"), "upgrade") {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		return false
	}
	return true
}

func hasHeaderToken(headerValue string, token string) bool {
	for _, item := range strings.Split(headerValue, ",") {
		if strings.EqualFold(strings.TrimSpace(item), token) {
			return true
		}
	}
	return false
}

func computeWebSocketAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func registerWSClient(client *wsClient) {
	wsClientsLock.Lock()
	wsClients[client] = struct{}{}
	wsClientsLock.Unlock()
}

func unregisterWSClient(client *wsClient) {
	wsClientsLock.Lock()
	delete(wsClients, client)
	wsClientsLock.Unlock()
}

func broadcastSlide(data ImageData) {
	broadcastPayload("slide", data)
}

func broadcastWeather(data WeatherData) {
	broadcastPayload("weather", data)
}

func broadcastPayload(messageType string, data interface{}) {
	wsClientsLock.Lock()
	clients := make([]*wsClient, 0, len(wsClients))
	for client := range wsClients {
		clients = append(clients, client)
	}
	wsClientsLock.Unlock()

	for _, client := range clients {
		if err := sendPayloadToClient(client, messageType, data); err != nil {
			log.Printf("WebSocket broadcast failed, dropping client: %v", err)
			client.conn.Close()
			unregisterWSClient(client)
		}
	}
}

func sendSlideToClient(client *wsClient, data ImageData) error {
	return sendPayloadToClient(client, "slide", data)
}

func sendWeatherToClient(client *wsClient, data WeatherData) error {
	return sendPayloadToClient(client, "weather", data)
}

func sendPayloadToClient(client *wsClient, messageType string, data interface{}) error {
	msg := slideMessage{Type: messageType, Data: data}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal websocket message: %w", err)
	}
	return writeWebSocketFrame(client, opcodeText, payload)
}

func consumeWebSocketFrames(client *wsClient, reader *bufio.Reader) error {
	for {
		opcode, payload, err := readWebSocketFrame(reader)
		if err != nil {
			return err
		}

		switch opcode {
		case opcodePing:
			if err := writeWebSocketFrame(client, opcodePong, payload); err != nil {
				return err
			}
		case opcodeClose:
			_ = writeWebSocketFrame(client, opcodeClose, payload)
			return fmt.Errorf("client requested close")
		default:
			// Text/binary frames are ignored; server is push-only for slideshow events.
		}
	}
}

func readWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}

	fin := header[0]&0x80 != 0
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	payloadLen := uint64(header[1] & 0x7F)

	if !fin {
		return 0, nil, fmt.Errorf("fragmented frames are not supported")
	}
	if !masked {
		return 0, nil, fmt.Errorf("client frame must be masked")
	}

	if payloadLen == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(ext))
	} else if payloadLen == 127 {
		ext := make([]byte, 8)
		if _, err := io.ReadFull(reader, ext); err != nil {
			return 0, nil, err
		}
		payloadLen = binary.BigEndian.Uint64(ext)
	}

	maskKey := make([]byte, 4)
	if _, err := io.ReadFull(reader, maskKey); err != nil {
		return 0, nil, err
	}

	if payloadLen > 1024*1024 {
		return 0, nil, fmt.Errorf("payload too large: %d", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= maskKey[i%4]
	}

	return opcode, payload, nil
}

func writeWebSocketFrame(client *wsClient, opcode byte, payload []byte) error {
	frame := make([]byte, 0, 10+len(payload))
	frame = append(frame, 0x80|opcode)

	payloadLen := len(payload)
	switch {
	case payloadLen <= 125:
		frame = append(frame, byte(payloadLen))
	case payloadLen <= 65535:
		frame = append(frame, 126)
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(payloadLen))
		frame = append(frame, ext...)
	default:
		frame = append(frame, 127)
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(payloadLen))
		frame = append(frame, ext...)
	}

	frame = append(frame, payload...)

	client.mu.Lock()
	defer client.mu.Unlock()

	_ = client.conn.SetWriteDeadline(time.Now().Add(wsWriteWindow))
	_, err := client.conn.Write(frame)
	_ = client.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return err
	}
	return nil
}
