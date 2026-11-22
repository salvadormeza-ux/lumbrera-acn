package main

// Destinos de transmisión de Atalaya: YouTube, Facebook, Restream (varias redes a la vez)
// o un servidor RTMP personalizado. Las claves se guardan solo en este PC.

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

type Destino struct {
	Servidor string `json:"servidor"`
	Clave    string `json:"clave"`
}

type Destinos struct {
	Activo string             `json:"activo"`
	Lista  map[string]Destino `json:"lista"`
}

var servidoresFijos = map[string]string{
	"youtube":  "rtmp://a.rtmp.youtube.com/live2",
	"facebook": "rtmps://live-api-s.facebook.com:443/rtmp/",
	"restream": "rtmp://live.restream.io/live",
}

var nombresDestino = map[string]string{"youtube": "YouTube", "facebook": "Facebook", "restream": "Restream (varias redes)", "personalizado": "Personalizado"}

func enmascarar(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "••••"
	}
	return "••••" + k[len(k)-4:]
}

func (t *Atalaya) rutaDestinos() string {
	return filepath.Join(filepath.Dir(t.cfgPath), "destinos.json")
}

func (t *Atalaya) leerDestinos() Destinos {
	d := Destinos{Lista: map[string]Destino{}}
	leerJSON(t.rutaDestinos(), &d)
	if d.Lista == nil {
		d.Lista = map[string]Destino{}
	}
	return d
}

func (t *Atalaya) rutasDestinos(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/destinos", auth(func(w http.ResponseWriter, r *http.Request) {
		d := t.leerDestinos()
		lista := []map[string]any{}
		for _, id := range []string{"youtube", "facebook", "restream", "personalizado"} {
			x := d.Lista[id]
			srv := x.Servidor
			if s, ok := servidoresFijos[id]; ok {
				srv = s
			}
			lista = append(lista, map[string]any{"id": id, "nombre": nombresDestino[id], "servidor": srv,
				"clave": enmascarar(x.Clave), "configurado": x.Clave != ""})
		}
		obs := map[string]any{}
		if o := t.obsActual(); o != nil {
			var s struct {
				Tipo string `json:"streamServiceType"`
				Cfg  struct {
					Server  string `json:"server"`
					Key     string `json:"key"`
					Service string `json:"service"`
				} `json:"streamServiceSettings"`
			}
			if o.pedir("GetStreamServiceSettings", nil, &s) == nil {
				srv := s.Cfg.Server
				if s.Cfg.Service != "" {
					srv = s.Cfg.Service + " · " + srv
				}
				obs = map[string]any{"tipo": s.Tipo, "servidor": srv, "clave": enmascarar(s.Cfg.Key)}
			}
		}
		jsonOK(w, map[string]any{"activo": d.Activo, "destinos": lista, "obs": obs})
	}))
	mux.HandleFunc("POST /api/destinos", auth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID       string `json:"id"`
			Clave    string `json:"clave"`
			Servidor string `json:"servidor"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || nombresDestino[req.ID] == "" {
			jsonError(w, 400, "destino inválido")
			return
		}
		d := t.leerDestinos()
		x := d.Lista[req.ID]
		if c := strings.TrimSpace(req.Clave); c != "" {
			x.Clave = c
		}
		if req.ID == "personalizado" {
			s := strings.TrimSpace(req.Servidor)
			if s != "" && !strings.HasPrefix(s, "rtmp://") && !strings.HasPrefix(s, "rtmps://") && !strings.HasPrefix(s, "srt://") {
				jsonError(w, 400, "el servidor debe empezar con rtmp://, rtmps:// o srt://")
				return
			}
			if s != "" {
				x.Servidor = s
			}
		}
		d.Lista[req.ID] = x
		if err := escribirJSON(t.rutaDestinos(), d); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		t.anotar("Clave guardada para %s", nombresDestino[req.ID])
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/destino-aplicar", auth(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string }
		json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
		o := t.obsActual()
		if o == nil {
			jsonError(w, 503, "OBS no está conectado")
			return
		}
		var st struct {
			Activo bool `json:"outputActive"`
		}
		if o.pedir("GetStreamStatus", nil, &st) == nil && st.Activo {
			jsonError(w, 409, "Detenga la transmisión antes de cambiar el destino")
			return
		}
		d := t.leerDestinos()
		x, ok := d.Lista[req.ID]
		srv := x.Servidor
		if s, fijo := servidoresFijos[req.ID]; fijo {
			srv = s
		}
		if !ok || x.Clave == "" || srv == "" {
			jsonError(w, 400, "Primero guarde la clave de transmisión de "+nombresDestino[req.ID])
			return
		}
		err := o.pedir("SetStreamServiceSettings", map[string]any{
			"streamServiceType":     "rtmp_custom",
			"streamServiceSettings": map[string]any{"server": srv, "key": x.Clave, "use_auth": false},
		}, nil)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		d.Activo = req.ID
		escribirJSON(t.rutaDestinos(), d)
		t.anotar("Destino de transmisión: %s", nombresDestino[req.ID])
		jsonOK(w, map[string]bool{"ok": true})
	}))
}
