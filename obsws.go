package main

// Cliente mínimo de WebSocket (RFC 6455) y del protocolo obs-websocket v5.

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type wsConn struct {
	c    net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
}

func wsDial(host string, port int, timeout time.Duration) (*wsConn, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	k := make([]byte, 16)
	rand.Read(k)
	key := base64.StdEncoding.EncodeToString(k)
	req := "GET / HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: obswebsocket.json\r\n\r\n"
	c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write([]byte(req)); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if resp.StatusCode != 101 {
		c.Close()
		return nil, fmt.Errorf("OBS respondió %s", resp.Status)
	}
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(h[:]) {
		c.Close()
		return nil, errors.New("respuesta WebSocket inválida")
	}
	c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: br}, nil
}

func (w *wsConn) escribir(op byte, data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	var h []byte
	h = append(h, 0x80|op)
	n := len(data)
	switch {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n < 65536:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = append(h, 0x80|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		h = append(h, b[:]...)
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	h = append(h, mask...)
	out := make([]byte, n)
	for i := range data {
		out[i] = data[i] ^ mask[i%4]
	}
	w.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := w.c.Write(append(h, out...))
	return err
}

func (w *wsConn) leerMensaje() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		op := h[0] & 0x0f
		n := uint64(h[1] & 0x7f)
		enmasc := h[1]&0x80 != 0
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 64<<20 {
			return nil, errors.New("mensaje demasiado grande")
		}
		var mask [4]byte
		if enmasc {
			io.ReadFull(w.r, mask[:])
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.r, p); err != nil {
			return nil, err
		}
		if enmasc {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8:
			return nil, errors.New("OBS cerró la conexión")
		case 0x9:
			w.escribir(0xA, p)
			continue
		case 0xA:
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}

func (w *wsConn) cerrar() {
	w.escribir(0x8, []byte{0x03, 0xe8})
	w.c.Close()
}

// ---------- protocolo OBS ----------

type OBS struct {
	mu        sync.Mutex
	ws        *wsConn
	pend      map[string]chan obsRespuesta
	sigID     int
	eventos   chan obsEvento
	conectado bool
	version   string
}

type obsRespuesta struct {
	OK      bool
	Code    int
	Comment string
	Data    json.RawMessage
}

type obsEvento struct {
	Tipo string
	Data json.RawMessage
}

func conectarOBS(host string, port int, password string) (*OBS, error) {
	ws, err := wsDial(host, port, 5*time.Second)
	if err != nil {
		return nil, err
	}
	ws.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	raw, err := ws.leerMensaje()
	if err != nil {
		ws.c.Close()
		return nil, err
	}
	var hola struct {
		Op int `json:"op"`
		D  struct {
			OBSWebSocketVersion string `json:"obsWebSocketVersion"`
			RPCVersion          int    `json:"rpcVersion"`
			Authentication      *struct {
				Challenge string `json:"challenge"`
				Salt      string `json:"salt"`
			} `json:"authentication"`
		} `json:"d"`
	}
	if err := json.Unmarshal(raw, &hola); err != nil || hola.Op != 0 {
		ws.c.Close()
		return nil, errors.New("OBS no envió el saludo esperado (¿obs-websocket 5?)")
	}
	ident := map[string]any{"rpcVersion": 1, "eventSubscriptions": 1 | 4 | 8 | 64}
	if hola.D.Authentication != nil {
		if password == "" {
			ws.c.Close()
			return nil, errors.New("OBS pide contraseña: cópiela en atalaya\\config.json")
		}
		s1 := sha256.Sum256([]byte(password + hola.D.Authentication.Salt))
		secreto := base64.StdEncoding.EncodeToString(s1[:])
		s2 := sha256.Sum256([]byte(secreto + hola.D.Authentication.Challenge))
		ident["authentication"] = base64.StdEncoding.EncodeToString(s2[:])
	}
	b, _ := json.Marshal(map[string]any{"op": 1, "d": ident})
	if err := ws.escribir(0x1, b); err != nil {
		ws.c.Close()
		return nil, err
	}
	raw, err = ws.leerMensaje()
	if err != nil {
		ws.c.Close()
		if strings.Contains(err.Error(), "cerró") {
			return nil, errors.New("OBS rechazó la contraseña")
		}
		return nil, err
	}
	var ok struct{ Op int }
	json.Unmarshal(raw, &ok)
	if ok.Op != 2 {
		ws.c.Close()
		return nil, errors.New("OBS no confirmó la identificación")
	}
	ws.c.SetReadDeadline(time.Time{})
	o := &OBS{ws: ws, pend: map[string]chan obsRespuesta{}, eventos: make(chan obsEvento, 64), conectado: true, version: hola.D.OBSWebSocketVersion}
	go o.bucle()
	return o, nil
}

func (o *OBS) bucle() {
	for {
		raw, err := o.ws.leerMensaje()
		if err != nil {
			o.mu.Lock()
			o.conectado = false
			for k, c := range o.pend {
				close(c)
				delete(o.pend, k)
			}
			o.mu.Unlock()
			close(o.eventos)
			return
		}
		var m struct {
			Op int `json:"op"`
			D  struct {
				RequestID     string          `json:"requestId"`
				RequestStatus struct {
					Result  bool   `json:"result"`
					Code    int    `json:"code"`
					Comment string `json:"comment"`
				} `json:"requestStatus"`
				ResponseData json.RawMessage `json:"responseData"`
				EventType    string          `json:"eventType"`
				EventData    json.RawMessage `json:"eventData"`
			} `json:"d"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		switch m.Op {
		case 7:
			o.mu.Lock()
			c := o.pend[m.D.RequestID]
			delete(o.pend, m.D.RequestID)
			o.mu.Unlock()
			if c != nil {
				c <- obsRespuesta{m.D.RequestStatus.Result, m.D.RequestStatus.Code, m.D.RequestStatus.Comment, m.D.ResponseData}
			}
		case 5:
			select {
			case o.eventos <- obsEvento{m.D.EventType, m.D.EventData}:
			default:
			}
		}
	}
}

func (o *OBS) pedir(tipo string, datos any, salida any) error {
	o.mu.Lock()
	if !o.conectado {
		o.mu.Unlock()
		return errors.New("sin conexión con OBS")
	}
	o.sigID++
	id := fmt.Sprintf("r%d", o.sigID)
	c := make(chan obsRespuesta, 1)
	o.pend[id] = c
	o.mu.Unlock()
	d := map[string]any{"requestType": tipo, "requestId": id}
	if datos != nil {
		d["requestData"] = datos
	}
	b, _ := json.Marshal(map[string]any{"op": 6, "d": d})
	if err := o.ws.escribir(0x1, b); err != nil {
		return err
	}
	select {
	case r, ok := <-c:
		if !ok {
			return errors.New("se perdió la conexión con OBS")
		}
		if !r.OK {
			return fmt.Errorf("OBS: %s (código %d)", r.Comment, r.Code)
		}
		if salida != nil && len(r.Data) > 0 {
			return json.Unmarshal(r.Data, salida)
		}
		return nil
	case <-time.After(10 * time.Second):
		o.mu.Lock()
		delete(o.pend, id)
		o.mu.Unlock()
		return errors.New("OBS no respondió a tiempo")
	}
}

func (o *OBS) cerrar() {
	o.ws.cerrar()
}
