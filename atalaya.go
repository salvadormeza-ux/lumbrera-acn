package main

// Atalaya: bot / panel de transmisión que controla OBS por WebSocket.

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Rutina struct {
	EscenaInicio string `json:"escena_inicio"`
	EscenaFin    string `json:"escena_fin"`
	Grabar       bool   `json:"grabar"`
}

type Atalaya struct {
	mu       sync.Mutex
	cfg      ConfigAtalaya
	cfgPath  string
	rutina   Rutina
	rutPath  string
	obs      *OBS
	ultError string
	est      EstadoOBS
	sesiones map[string]time.Time
	fallos   map[string][]time.Time
	bitacora []string
}

type AudioOBS struct {
	Nombre   string `json:"nombre"`
	Silencio bool   `json:"silencio"`
}

type EstadoOBS struct {
	Conectado      bool       `json:"conectado"`
	Error          string     `json:"error"`
	Version        string     `json:"version"`
	Escenas        []string   `json:"escenas"`
	EscenaActual   string     `json:"escena_actual"`
	Transmitiendo  bool       `json:"transmitiendo"`
	TiempoTrans    string     `json:"tiempo_transmision"`
	Congestion     float64    `json:"congestion"`
	FramesPerdidos int        `json:"frames_perdidos"`
	Grabando       bool       `json:"grabando"`
	TiempoGrab     string     `json:"tiempo_grabacion"`
	Audios         []AudioOBS `json:"audios"`
	LumbreraOK     bool       `json:"lumbrera_ok"`
	Bitacora       []string   `json:"bitacora"`
	Rutina         Rutina     `json:"rutina"`
	Actualizado    int64      `json:"actualizado"`
}

func nuevaAtalaya(dir string) (*Atalaya, error) {
	t := &Atalaya{cfgPath: filepath.Join(dir, "config.json"), rutPath: filepath.Join(dir, "rutina.json"),
		sesiones: map[string]time.Time{}, fallos: map[string][]time.Time{}}
	t.cfg = configAtalayaPorDefecto()
	if err := leerJSON(t.cfgPath, &t.cfg); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("atalaya\\config.json con error: %v", err)
		}
		escribirJSON(t.cfgPath, t.cfg)
	}
	t.rutina.Grabar = true
	leerJSON(t.rutPath, &t.rutina)
	return t, nil
}

func (t *Atalaya) anotar(f string, a ...any) {
	msg := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(f, a...)
	t.mu.Lock()
	t.bitacora = append([]string{msg}, t.bitacora...)
	if len(t.bitacora) > 30 {
		t.bitacora = t.bitacora[:30]
	}
	t.mu.Unlock()
	logf("[Atalaya] %s", fmt.Sprintf(f, a...))
}

func (t *Atalaya) obsActual() *OBS {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.obs != nil && t.obs.conectado {
		return t.obs
	}
	return nil
}

// bucleConexion mantiene la conexión con OBS y refresca el estado.
func (t *Atalaya) bucleConexion() {
	for {
		o := t.obsActual()
		if o == nil {
			t.mu.Lock()
			c := t.cfg
			t.mu.Unlock()
			nuevo, err := conectarOBS(c.OBS.Host, c.OBS.Puerto, c.OBS.Password)
			t.mu.Lock()
			if err != nil {
				msg := err.Error()
				if !strings.Contains(msg, "contraseña") && (strings.Contains(msg, "refused") || strings.Contains(msg, "connectex") || strings.Contains(msg, "actively") || strings.Contains(msg, "deneg")) {
					msg = "OBS no está abierto o el servidor WebSocket no está habilitado (Herramientas → Ajustes del servidor WebSocket)"
				}
				if t.ultError != msg {
					t.ultError = msg
					t.mu.Unlock()
					t.anotar("Sin conexión: %s", msg)
					t.mu.Lock()
				}
				t.est.Conectado = false
				t.mu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}
			t.obs = nuevo
			t.ultError = ""
			t.mu.Unlock()
			t.anotar("Conectado a OBS (obs-websocket %s)", nuevo.version)
			go func(o *OBS) {
				for range o.eventos {
					// los eventos solo disparan un refresco
					t.refrescar()
				}
			}(nuevo)
			o = nuevo
		}
		t.refrescar()
		time.Sleep(1500 * time.Millisecond)
	}
}

func durHMS(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s/60)%60, s%60)
}

func (t *Atalaya) refrescar() {
	o := t.obsActual()
	e := EstadoOBS{Conectado: o != nil}
	if o != nil {
		e.Version = o.version
		var esc struct {
			Actual  string `json:"currentProgramSceneName"`
			Escenas []struct {
				Nombre string `json:"sceneName"`
				Indice int    `json:"sceneIndex"`
			} `json:"scenes"`
		}
		if err := o.pedir("GetSceneList", nil, &esc); err == nil {
			sort.Slice(esc.Escenas, func(i, j int) bool { return esc.Escenas[i].Indice > esc.Escenas[j].Indice })
			for _, s := range esc.Escenas {
				e.Escenas = append(e.Escenas, s.Nombre)
			}
			e.EscenaActual = esc.Actual
		}
		var st struct {
			Activo     bool    `json:"outputActive"`
			Duracion   int64   `json:"outputDuration"`
			Congestion float64 `json:"outputCongestion"`
			Saltados   int     `json:"outputSkippedFrames"`
		}
		if o.pedir("GetStreamStatus", nil, &st) == nil {
			e.Transmitiendo, e.TiempoTrans, e.Congestion, e.FramesPerdidos = st.Activo, durHMS(st.Duracion), st.Congestion, st.Saltados
		}
		var rec struct {
			Activo   bool  `json:"outputActive"`
			Duracion int64 `json:"outputDuration"`
		}
		if o.pedir("GetRecordStatus", nil, &rec) == nil {
			e.Grabando, e.TiempoGrab = rec.Activo, durHMS(rec.Duracion)
		}
		var ins struct {
			Inputs []struct {
				Nombre string `json:"inputName"`
				Tipo   string `json:"inputKind"`
			} `json:"inputs"`
		}
		if o.pedir("GetInputList", nil, &ins) == nil {
			for _, in := range ins.Inputs {
				k := strings.ToLower(in.Tipo)
				if !(strings.Contains(k, "audio") || strings.Contains(k, "wasapi") || strings.Contains(k, "pulse") || strings.Contains(k, "alsa") || strings.Contains(k, "coreaudio")) {
					continue
				}
				var m struct {
					Mute bool `json:"inputMuted"`
				}
				if o.pedir("GetInputMute", map[string]any{"inputName": in.Nombre}, &m) == nil {
					e.Audios = append(e.Audios, AudioOBS{in.Nombre, m.Mute})
				}
			}
		}
	}
	// Lumbrera
	t.mu.Lock()
	url := strings.TrimRight(t.cfg.LumbreraURL, "/")
	t.mu.Unlock()
	cli := http.Client{Timeout: 1500 * time.Millisecond}
	if r, err := cli.Get(url + "/api/ping"); err == nil {
		e.LumbreraOK = r.StatusCode == 200
		r.Body.Close()
	}
	t.mu.Lock()
	e.Error = t.ultError
	e.Bitacora = append([]string(nil), t.bitacora...)
	e.Rutina = t.rutina
	e.Actualizado = time.Now().UnixMilli()
	t.est = e
	t.mu.Unlock()
}

func (t *Atalaya) autenticado(r *http.Request) bool {
	c, err := r.Cookie("atalaya_sesion")
	if err != nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	exp, ok := t.sesiones[c.Value]
	if !ok || time.Now().After(exp) {
		return false
	}
	t.sesiones[c.Value] = time.Now().Add(7 * 24 * time.Hour)
	return true
}

func (t *Atalaya) rutas() http.Handler {
	mux := http.NewServeMux()
	pagina := func(n string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := webFS.ReadFile("web/" + n)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		}
	}
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !t.autenticado(r) {
				jsonError(w, 401, "Sesión expirada")
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !t.autenticado(r) {
			pagina("ingreso.html")(w, r)
			return
		}
		pagina("atalaya.html")(w, r)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(mustSub("web")))))
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]string{"app": "atalaya", "version": version})
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Clave string }
		json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
		ip := ipCliente(r)
		t.mu.Lock()
		rec := t.fallos[ip][:0]
		for _, x := range t.fallos[ip] {
			if time.Since(x) < 5*time.Minute {
				rec = append(rec, x)
			}
		}
		t.fallos[ip] = rec
		bloq := len(rec) >= 8
		clave := t.cfg.Clave
		t.mu.Unlock()
		if bloq {
			jsonError(w, 429, "Demasiados intentos. Espere 5 minutos.")
			return
		}
		if subtle.ConstantTimeCompare([]byte(req.Clave), []byte(clave)) != 1 {
			t.mu.Lock()
			t.fallos[ip] = append(t.fallos[ip], time.Now())
			t.mu.Unlock()
			time.Sleep(700 * time.Millisecond)
			jsonError(w, 401, "Clave incorrecta")
			return
		}
		tok := tokenAleatorio()
		t.mu.Lock()
		t.sesiones[tok] = time.Now().Add(7 * 24 * time.Hour)
		t.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "atalaya_sesion", Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
		jsonOK(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/estado", auth(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		e := t.est
		e.Bitacora = append([]string(nil), t.bitacora...)
		e.Error = t.ultError
		lu := t.cfg.LumbreraURL
		t.mu.Unlock()
		jsonOK(w, map[string]any{"obs": e, "version": version, "lumbrera_url": lu})
	}))
	accion := func(f func(o *OBS, body map[string]any) (string, error)) http.HandlerFunc {
		return auth(func(w http.ResponseWriter, r *http.Request) {
			o := t.obsActual()
			if o == nil {
				jsonError(w, 503, "OBS no está conectado")
				return
			}
			body := map[string]any{}
			json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
			msg, err := f(o, body)
			if err != nil {
				t.anotar("Error: %v", err)
				jsonError(w, 400, err.Error())
				return
			}
			if msg != "" {
				t.anotar("%s", msg)
			}
			go t.refrescar()
			jsonOK(w, map[string]bool{"ok": true})
		})
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	mux.HandleFunc("POST /api/escena", accion(func(o *OBS, b map[string]any) (string, error) {
		n := str(b, "nombre")
		return "Escena: " + n, o.pedir("SetCurrentProgramScene", map[string]any{"sceneName": n}, nil)
	}))
	mux.HandleFunc("POST /api/transmision", accion(func(o *OBS, b map[string]any) (string, error) {
		if str(b, "accion") == "iniciar" {
			return "Transmisión iniciada", o.pedir("StartStream", nil, nil)
		}
		return "Transmisión detenida", o.pedir("StopStream", nil, nil)
	}))
	mux.HandleFunc("POST /api/grabacion", accion(func(o *OBS, b map[string]any) (string, error) {
		if str(b, "accion") == "iniciar" {
			return "Grabación iniciada", o.pedir("StartRecord", nil, nil)
		}
		return "Grabación detenida", o.pedir("StopRecord", nil, nil)
	}))
	mux.HandleFunc("POST /api/silencio", accion(func(o *OBS, b map[string]any) (string, error) {
		n := str(b, "nombre")
		return "Audio alternado: " + n, o.pedir("ToggleInputMute", map[string]any{"inputName": n}, nil)
	}))
	mux.HandleFunc("POST /api/fuente-lumbrera", accion(func(o *OBS, b map[string]any) (string, error) {
		var esc struct {
			Actual string `json:"currentProgramSceneName"`
		}
		if err := o.pedir("GetCurrentProgramScene", nil, &esc); err != nil {
			return "", err
		}
		t.mu.Lock()
		url := strings.TrimRight(t.cfg.LumbreraURL, "/") + "/obs"
		t.mu.Unlock()
		err := o.pedir("CreateInput", map[string]any{
			"sceneName": esc.Actual, "inputName": "Letras Lumbrera", "inputKind": "browser_source",
			"inputSettings":    map[string]any{"url": url, "width": 1920, "height": 1080, "reroute_audio": false},
			"sceneItemEnabled": true,
		}, nil)
		if err != nil && strings.Contains(err.Error(), "601") {
			return "", errors.New("ya existe una fuente llamada «Letras Lumbrera»; agréguela a la escena desde OBS (Agregar → Fuente existente)")
		}
		return "Fuente «Letras Lumbrera» agregada a la escena " + esc.Actual, err
	}))
	mux.HandleFunc("POST /api/rutina", auth(func(w http.ResponseWriter, r *http.Request) {
		var ru Rutina
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&ru); err != nil {
			jsonError(w, 400, "datos inválidos")
			return
		}
		t.mu.Lock()
		t.rutina = ru
		t.est.Rutina = ru
		t.mu.Unlock()
		escribirJSON(t.rutPath, ru)
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/culto", accion(func(o *OBS, b map[string]any) (string, error) {
		t.mu.Lock()
		ru := t.rutina
		t.mu.Unlock()
		if str(b, "accion") == "iniciar" {
			if ru.EscenaInicio != "" {
				if err := o.pedir("SetCurrentProgramScene", map[string]any{"sceneName": ru.EscenaInicio}, nil); err != nil {
					return "", err
				}
			}
			if ru.Grabar {
				if err := o.pedir("StartRecord", nil, nil); err != nil && !strings.Contains(err.Error(), "500") {
					t.anotar("Grabación: %v", err)
				}
			}
			if err := o.pedir("StartStream", nil, nil); err != nil && !strings.Contains(err.Error(), "500") {
				return "", err
			}
			return "▶ Culto iniciado (transmisión" + map[bool]string{true: " + grabación", false: ""}[ru.Grabar] + ")", nil
		}
		if ru.EscenaFin != "" {
			o.pedir("SetCurrentProgramScene", map[string]any{"sceneName": ru.EscenaFin}, nil)
			time.Sleep(4 * time.Second) // deja ver la escena final unos segundos
		}
		o.pedir("StopStream", nil, nil)
		if ru.Grabar {
			o.pedir("StopRecord", nil, nil)
		}
		return "■ Culto terminado", nil
	}))
	t.rutasDestinos(mux, auth)
	return cabecerasSeguras(mux)
}

func servirAtalaya(t *Atalaya) error {
	go t.bucleConexion()
	srv := &http.Server{Addr: fmt.Sprintf(":%d", t.cfg.Puerto), Handler: t.rutas(), ReadHeaderTimeout: 10 * time.Second}
	logf("Atalaya %s escuchando en el puerto %d", version, t.cfg.Puerto)
	return srv.ListenAndServe()
}
