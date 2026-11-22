package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed web
var webFS embed.FS

//go:embed recursos/RV1909.json.gz
var bibliaIncluida []byte

// ---------- estado en vivo ----------

type Lamina struct {
	Texto    string `json:"texto"`
	Ref      string `json:"ref"`
	Etiqueta string `json:"etiqueta,omitempty"`
}

type Estado struct {
	Rev         int64   `json:"rev"`
	Ahora       int64   `json:"ahora"` // ms del servidor, para sincronizar relojes
	Modo        string  `json:"modo"`  // texto | negro | limpio | logo
	Tipo        string  `json:"tipo"`  // cancion | biblia | texto
	Actual      *Lamina `json:"actual"`
	Siguiente   *Lamina `json:"siguiente"`
	Aviso       string  `json:"aviso"`
	AvisoActivo bool    `json:"aviso_activo"`
	Cuenta      struct {
		Activa bool   `json:"activa"`
		Fin    int64  `json:"fin"`
		Texto  string `json:"texto"`
	} `json:"cuenta"`
	Crono struct {
		Corriendo  bool  `json:"corriendo"`
		Inicio     int64 `json:"inicio"`
		Acumulado  int64 `json:"acumulado"`
		LimiteMin  int   `json:"limite_min"`
	} `json:"crono"`
	MensajeEscenario string `json:"mensaje_escenario"`
	Estilo           Estilo `json:"estilo"`
	Iglesia          string `json:"iglesia"`
}

type App struct {
	mu        sync.Mutex
	cfg       ConfigLumbrera
	cfgPath   string
	datos     string
	biblias   *Biblioteca
	canciones *Cancionero
	estado    Estado
	clientes  map[chan []byte]struct{}
	sesiones  map[string]time.Time
	fallos    map[string][]time.Time
}

func nuevaApp(dir string) (*App, error) {
	a := &App{
		cfgPath:  filepath.Join(dir, "config.json"),
		datos:    filepath.Join(dir, "datos"),
		clientes: map[chan []byte]struct{}{},
		sesiones: map[string]time.Time{},
		fallos:   map[string][]time.Time{},
	}
	a.cfg = configLumbreraPorDefecto()
	if err := leerJSON(a.cfgPath, &a.cfg); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config.json con error: %v", err)
		}
		escribirJSON(a.cfgPath, a.cfg)
	}
	for _, d := range []string{"biblias", "canciones", "fondos"} {
		os.MkdirAll(filepath.Join(a.datos, d), 0o755)
	}
	asegurarBibliaIncluida(filepath.Join(a.datos, "biblias"))
	a.biblias = nuevaBiblioteca(filepath.Join(a.datos, "biblias"))
	a.canciones = nuevoCancionero(filepath.Join(a.datos, "canciones"))
	a.estado.Modo = "limpio"
	a.estado.Estilo = a.cfg.Estilo
	a.estado.Iglesia = a.cfg.Iglesia
	a.estado.Cuenta.Texto = "El culto comienza en"
	a.estado.Crono.LimiteMin = 40
	return a, nil
}

func asegurarBibliaIncluida(dir string) {
	p := filepath.Join(dir, "RV1909.json.gz")
	if _, err := os.Stat(p); err == nil {
		return
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(p, bibliaIncluida, 0o644)
}

func (a *App) emitir() {
	// requiere a.mu tomado
	a.estado.Rev++
	a.estado.Ahora = time.Now().UnixMilli()
	b, _ := json.Marshal(a.estado)
	for c := range a.clientes {
		select {
		case c <- b:
		default:
		}
	}
}

// ---------- sesiones ----------

const cookieSesion = "lumbrera_sesion"

func tokenAleatorio() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func ipCliente(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (a *App) autenticado(r *http.Request) bool {
	c, err := r.Cookie(cookieSesion)
	if err != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sesiones[c.Value]
	if !ok || time.Now().After(exp) {
		delete(a.sesiones, c.Value)
		return false
	}
	a.sesiones[c.Value] = time.Now().Add(7 * 24 * time.Hour)
	return true
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var req struct{ Clave string }
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	ip := ipCliente(r)
	a.mu.Lock()
	recientes := a.fallos[ip][:0]
	for _, t := range a.fallos[ip] {
		if time.Since(t) < 5*time.Minute {
			recientes = append(recientes, t)
		}
	}
	a.fallos[ip] = recientes
	bloqueado := len(recientes) >= 8
	clave := a.cfg.Clave
	a.mu.Unlock()
	if bloqueado {
		jsonError(w, 429, "Demasiados intentos. Espere 5 minutos.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Clave), []byte(clave)) != 1 {
		a.mu.Lock()
		a.fallos[ip] = append(a.fallos[ip], time.Now())
		a.mu.Unlock()
		time.Sleep(700 * time.Millisecond)
		jsonError(w, 401, "Clave incorrecta")
		return
	}
	tok := tokenAleatorio()
	a.mu.Lock()
	a.sesiones[tok] = time.Now().Add(7 * 24 * time.Hour)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieSesion, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	jsonOK(w, map[string]any{"ok": true})
}

func (a *App) conAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.autenticado(r) {
			jsonError(w, 401, "Sesión expirada")
			return
		}
		h(w, r)
	}
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func leerCuerpo(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(v)
}

// ---------- rutas ----------

func (a *App) rutas() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	estaticos := http.FileServer(http.FS(sub))

	pagina := func(nombre string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := webFS.ReadFile("web/" + nombre)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !a.autenticado(r) {
			pagina("ingreso.html")(w, r)
			return
		}
		pagina("consola.html")(w, r)
	})
	mux.HandleFunc("GET /proyector", pagina("proyector.html"))
	mux.HandleFunc("GET /escenario", pagina("escenario.html"))
	mux.HandleFunc("GET /obs", pagina("obs.html"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", estaticos))
	mux.HandleFunc("GET /fondos/{nombre}", func(w http.ResponseWriter, r *http.Request) {
		n := filepath.Base(r.PathValue("nombre"))
		w.Header().Set("Cache-Control", "max-age=3600")
		http.ServeFile(w, r, filepath.Join(a.datos, "fondos", n))
	})
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, map[string]string{"app": "lumbrera", "version": version})
	})
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieSesion); err == nil {
			a.mu.Lock()
			delete(a.sesiones, c.Value)
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: cookieSesion, Value: "", Path: "/", MaxAge: -1})
		jsonOK(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/estado", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.estado.Ahora = time.Now().UnixMilli()
		e := a.estado
		a.mu.Unlock()
		jsonOK(w, e)
	})
	mux.HandleFunc("GET /api/eventos", a.eventos)

	// --- control en vivo ---
	mux.HandleFunc("POST /api/mostrar", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tipo      string  `json:"tipo"`
			Actual    *Lamina `json:"actual"`
			Siguiente *Lamina `json:"siguiente"`
		}
		if err := leerCuerpo(r, &req); err != nil || req.Actual == nil {
			jsonError(w, 400, "datos inválidos")
			return
		}
		a.mu.Lock()
		a.estado.Tipo, a.estado.Actual, a.estado.Siguiente = req.Tipo, req.Actual, req.Siguiente
		if a.estado.Modo != "negro" {
			a.estado.Modo = "texto"
		}
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/modo", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Modo string }
		leerCuerpo(r, &req)
		switch req.Modo {
		case "texto", "negro", "limpio", "logo":
		default:
			jsonError(w, 400, "modo inválido")
			return
		}
		a.mu.Lock()
		a.estado.Modo = req.Modo
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/aviso", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texto  string `json:"texto"`
			Activo bool   `json:"activo"`
		}
		leerCuerpo(r, &req)
		a.mu.Lock()
		a.estado.Aviso, a.estado.AvisoActivo = strings.TrimSpace(req.Texto), req.Activo && strings.TrimSpace(req.Texto) != ""
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/cuenta", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Minutos float64 `json:"minutos"`
			Texto   string  `json:"texto"`
			Activa  bool    `json:"activa"`
		}
		leerCuerpo(r, &req)
		a.mu.Lock()
		a.estado.Cuenta.Activa = req.Activa && req.Minutos > 0
		a.estado.Cuenta.Texto = req.Texto
		if a.estado.Cuenta.Activa {
			a.estado.Cuenta.Fin = time.Now().Add(time.Duration(req.Minutos * float64(time.Minute))).UnixMilli()
		}
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/crono", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Accion    string `json:"accion"`
			LimiteMin int    `json:"limite_min"`
		}
		leerCuerpo(r, &req)
		ahora := time.Now().UnixMilli()
		a.mu.Lock()
		c := &a.estado.Crono
		if req.LimiteMin > 0 {
			c.LimiteMin = req.LimiteMin
		}
		switch req.Accion {
		case "iniciar":
			if !c.Corriendo {
				c.Corriendo, c.Inicio = true, ahora
			}
		case "pausar":
			if c.Corriendo {
				c.Acumulado += ahora - c.Inicio
				c.Corriendo = false
			}
		case "reiniciar":
			c.Corriendo, c.Acumulado, c.Inicio = false, 0, 0
		}
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/escenario", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Mensaje string }
		leerCuerpo(r, &req)
		a.mu.Lock()
		a.estado.MensajeEscenario = strings.TrimSpace(req.Mensaje)
		a.emitir()
		a.mu.Unlock()
		jsonOK(w, map[string]bool{"ok": true})
	}))

	// --- información y ajustes ---
	mux.HandleFunc("GET /api/info", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()
		jsonOK(w, map[string]any{
			"version": version, "iglesia": cfg.Iglesia, "ips": ipsLocales(), "puerto": cfg.Puerto,
			"biblia_predeterminada": cfg.BibliaPred, "estilo": cfg.Estilo, "clave_inicial": cfg.Clave == "acn2026",
			"pantalla_proyector": cfg.PantallaProyector, "abrir_proyector": cfg.AbrirProyector,
			"fondos": a.listarFondos(),
		})
	}))
	mux.HandleFunc("POST /api/ajustes", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Iglesia     *string `json:"iglesia"`
			Estilo      *Estilo `json:"estilo"`
			BibliaPred  *string `json:"biblia_predeterminada"`
			ClaveActual string  `json:"clave_actual"`
			ClaveNueva  string  `json:"clave_nueva"`
			Pantalla    *int    `json:"pantalla_proyector"`
			AbrirProy   *bool   `json:"abrir_proyector"`
		}
		if err := leerCuerpo(r, &req); err != nil {
			jsonError(w, 400, "datos inválidos")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if req.ClaveNueva != "" {
			if subtle.ConstantTimeCompare([]byte(req.ClaveActual), []byte(a.cfg.Clave)) != 1 {
				jsonError(w, 403, "La clave actual no coincide")
				return
			}
			if len(req.ClaveNueva) < 6 {
				jsonError(w, 400, "La clave nueva debe tener al menos 6 caracteres")
				return
			}
			a.cfg.Clave = req.ClaveNueva
		}
		if req.Iglesia != nil {
			a.cfg.Iglesia = strings.TrimSpace(*req.Iglesia)
		}
		if req.Estilo != nil {
			e := *req.Estilo
			if e.TamanoMax < 30 || e.TamanoMax > 200 {
				e.TamanoMax = 96
			}
			if e.Oscurecer < 0 || e.Oscurecer > 90 {
				e.Oscurecer = 45
			}
			if e.OBSTamano < 20 || e.OBSTamano > 120 {
				e.OBSTamano = 54
			}
			e.ImagenFondo = filepath.Base(e.ImagenFondo)
			e.Logo = filepath.Base(e.Logo)
			if e.ImagenFondo == "." {
				e.ImagenFondo = ""
			}
			if e.Logo == "." {
				e.Logo = ""
			}
			a.cfg.Estilo = e
		}
		if req.BibliaPred != nil {
			a.cfg.BibliaPred = *req.BibliaPred
		}
		if req.Pantalla != nil {
			a.cfg.PantallaProyector = *req.Pantalla
		}
		if req.AbrirProy != nil {
			a.cfg.AbrirProyector = *req.AbrirProy
		}
		if err := escribirJSON(a.cfgPath, a.cfg); err != nil {
			jsonError(w, 500, "no se pudo guardar: "+err.Error())
			return
		}
		a.estado.Estilo, a.estado.Iglesia = a.cfg.Estilo, a.cfg.Iglesia
		a.emitir()
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/subir-imagen", a.conAuth(a.subirImagen))
	mux.HandleFunc("POST /api/abrir-proyector", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		if err := abrirProyector(a); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/pantallas", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, listarPantallas())
	}))

	// --- biblias ---
	mux.HandleFunc("GET /api/biblias", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, a.biblias.lista())
	}))
	mux.HandleFunc("GET /api/libros", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		bb := a.biblias.obtener(r.URL.Query().Get("biblia"))
		type L struct {
			LibroInfo
			Capitulos int `json:"capitulos"`
		}
		out := []L{}
		for i, l := range libros {
			n := 0
			if bb != nil && i < len(bb.Libros) {
				n = len(bb.Libros[i].Capitulos)
			}
			out = append(out, L{l, n})
		}
		jsonOK(w, out)
	}))
	mux.HandleFunc("GET /api/capitulo", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		bb := a.biblias.obtener(q.Get("biblia"))
		if bb == nil {
			jsonError(w, 404, "Biblia no encontrada")
			return
		}
		ref := Referencia{}
		if t := q.Get("ref"); t != "" {
			var ok bool
			ref, ok = parsearReferencia(t)
			if !ok {
				jsonError(w, 404, "No reconozco esa referencia")
				return
			}
		} else {
			ref.Libro, _ = strconv.Atoi(q.Get("libro"))
			ref.Capitulo, _ = strconv.Atoi(q.Get("capitulo"))
		}
		if ref.Libro < 1 || ref.Libro > 66 {
			jsonError(w, 404, "Libro inválido")
			return
		}
		caps := bb.Libros[ref.Libro-1].Capitulos
		if ref.Capitulo < 1 || ref.Capitulo > len(caps) {
			jsonError(w, 404, fmt.Sprintf("%s tiene %d capítulos", libros[ref.Libro-1].Nombre, len(caps)))
			return
		}
		jsonOK(w, map[string]any{
			"libro": ref.Libro, "nombre": libros[ref.Libro-1].Nombre, "capitulo": ref.Capitulo,
			"total_capitulos": len(caps), "desde": ref.VerDesde, "hasta": ref.VerHasta,
			"versiculos": caps[ref.Capitulo-1], "sigla": bb.Sigla,
		})
	}))
	mux.HandleFunc("GET /api/buscar-biblia", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		bb := a.biblias.obtener(r.URL.Query().Get("biblia"))
		if bb == nil {
			jsonError(w, 404, "Biblia no encontrada")
			return
		}
		res := bb.buscarTexto(r.URL.Query().Get("q"), 100)
		if res == nil {
			res = []ResultadoBusqueda{}
		}
		jsonOK(w, res)
	}))
	mux.HandleFunc("POST /api/biblias/importar", a.conAuth(a.importarBibliaHTTP))
	mux.HandleFunc("DELETE /api/biblias/{sigla}", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		if err := a.biblias.borrar(r.PathValue("sigla")); err != nil {
			jsonError(w, 404, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}))

	// --- canciones ---
	mux.HandleFunc("GET /api/canciones", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, a.canciones.buscar(r.URL.Query().Get("q")))
	}))
	mux.HandleFunc("GET /api/canciones/{id}", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		c := a.canciones.obtener(r.PathValue("id"))
		if c == nil {
			jsonError(w, 404, "no existe")
			return
		}
		jsonOK(w, c)
	}))
	mux.HandleFunc("POST /api/canciones", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var c Cancion
		if err := leerCuerpo(r, &c); err != nil {
			jsonError(w, 400, "datos inválidos")
			return
		}
		res, err := a.canciones.guardar(c)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		jsonOK(w, res)
	}))
	mux.HandleFunc("DELETE /api/canciones/{id}", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		if err := a.canciones.borrar(r.PathValue("id")); err != nil {
			jsonError(w, 404, err.Error())
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/canciones/importar", a.conAuth(a.importarCancionesHTTP))

	// --- lista del culto ---
	mux.HandleFunc("GET /api/lista", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var lista []map[string]any
		leerJSON(filepath.Join(a.datos, "lista.json"), &lista)
		if lista == nil {
			lista = []map[string]any{}
		}
		jsonOK(w, lista)
	}))
	mux.HandleFunc("POST /api/lista", a.conAuth(func(w http.ResponseWriter, r *http.Request) {
		var lista []map[string]any
		if err := leerCuerpo(r, &lista); err != nil {
			jsonError(w, 400, "datos inválidos")
			return
		}
		escribirJSON(filepath.Join(a.datos, "lista.json"), lista)
		jsonOK(w, map[string]bool{"ok": true})
	}))

	return cabecerasSeguras(mux)
}

func cabecerasSeguras(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

func (a *App) eventos(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "sin streaming", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	c := make(chan []byte, 8)
	a.mu.Lock()
	a.clientes[c] = struct{}{}
	a.estado.Ahora = time.Now().UnixMilli()
	ini, _ := json.Marshal(a.estado)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.clientes, c)
		a.mu.Unlock()
	}()
	fmt.Fprintf(w, "retry: 1500\ndata: %s\n\n", ini)
	fl.Flush()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-t.C:
			fmt.Fprintf(w, ": latido\n\n")
			fl.Flush()
		}
	}
}

func (a *App) listarFondos() []string {
	ents, _ := os.ReadDir(filepath.Join(a.datos, "fondos"))
	out := []string{}
	for _, e := range ents {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) subirImagen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	f, h, err := r.FormFile("archivo")
	if err != nil {
		jsonError(w, 400, "falta la imagen (máx. 25 MB)")
		return
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(h.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
	default:
		jsonError(w, 400, "use JPG, PNG, WEBP o GIF")
		return
	}
	nombre := nombreArchivoSeguro(strings.TrimSuffix(filepath.Base(h.Filename), filepath.Ext(h.Filename))) + ext
	dst, err := os.Create(filepath.Join(a.datos, "fondos", nombre))
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	io.Copy(dst, f)
	dst.Close()
	jsonOK(w, map[string]string{"nombre": nombre})
}

func (a *App) importarBibliaHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, 400, "archivo demasiado grande o inválido")
		return
	}
	var data []byte
	nombreArch := ""
	if f, h, err := r.FormFile("archivo"); err == nil {
		data, _ = io.ReadAll(f)
		f.Close()
		nombreArch = h.Filename
	} else if u := strings.TrimSpace(r.FormValue("url")); u != "" {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			jsonError(w, 400, "el enlace debe empezar con http:// o https://")
			return
		}
		cli := &http.Client{Timeout: 2 * time.Minute}
		resp, err := cli.Get(u)
		if err != nil {
			jsonError(w, 400, "no se pudo descargar: "+err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			jsonError(w, 400, "el enlace respondió "+resp.Status)
			return
		}
		data, _ = io.ReadAll(io.LimitReader(resp.Body, 200<<20))
		nombreArch = filepath.Base(resp.Request.URL.Path)
	} else {
		jsonError(w, 400, "seleccione un archivo o pegue un enlace")
		return
	}
	acc, err := importarBiblia(nombreArch, data)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	sigla := strings.ToUpper(strings.TrimSpace(r.FormValue("sigla")))
	nombre := strings.TrimSpace(r.FormValue("nombre"))
	if nombre == "" {
		nombre = acc.titulo
	}
	if nombre == "" {
		nombre = strings.TrimSuffix(nombreArch, filepath.Ext(nombreArch))
	}
	if sigla == "" {
		sigla = adivinarSigla(nombre + " " + nombreArch)
	}
	sigla = nombreArchivoSeguro(sigla)
	bb := acc.biblia(sigla, nombre)
	if err := a.biblias.guardar(bb); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	logf("Biblia importada: %s (%s) %d versículos, formato %s", sigla, nombre, acc.n, acc.formato)
	jsonOK(w, map[string]any{"sigla": sigla, "nombre": nombre, "versiculos": acc.n, "formato": acc.formato})
}

func adivinarSigla(s string) string {
	t := sinAcentos(s)
	switch {
	case strings.Contains(t, "1960") || strings.Contains(t, "rvr60") || strings.Contains(t, "rv60"):
		return "RVR1960"
	case strings.Contains(t, "1995") || strings.Contains(t, "rv95"):
		return "RVR1995"
	case strings.Contains(t, "1909"):
		return "RV1909"
	case strings.Contains(t, "nvi"):
		return "NVI"
	case strings.Contains(t, "ntv"):
		return "NTV"
	case strings.Contains(t, "lbla"):
		return "LBLA"
	case strings.Contains(t, "dhh"):
		return "DHH"
	}
	f := strings.Fields(s)
	if len(f) > 0 {
		x := strings.ToUpper(f[0])
		if len(x) > 10 {
			x = x[:10]
		}
		return x
	}
	return "BIBLIA"
}

func (a *App) importarCancionesHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		jsonError(w, 400, "archivos inválidos")
		return
	}
	n := 0
	var errores []string
	for _, fh := range r.MultipartForm.File["archivos"] {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(f, 2<<20))
		f.Close()
		texto := decodificarTexto(b)
		titulo := strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename))
		c := cancionDesdeTexto(titulo, texto)
		if _, err := a.canciones.guardar(c); err != nil {
			errores = append(errores, fh.Filename+": "+err.Error())
			continue
		}
		n++
	}
	jsonOK(w, map[string]any{"importadas": n, "errores": errores})
}

// decodificarTexto acepta UTF-8, UTF-16 con BOM o Windows-1252.
func decodificarTexto(b []byte) string {
	if len(b) >= 2 && (b[0] == 0xff && b[1] == 0xfe || b[0] == 0xfe && b[1] == 0xff) {
		le := b[0] == 0xff
		b = b[2:]
		r := make([]rune, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			var u uint16
			if le {
				u = uint16(b[i]) | uint16(b[i+1])<<8
			} else {
				u = uint16(b[i])<<8 | uint16(b[i+1])
			}
			r = append(r, rune(u))
		}
		return string(r)
	}
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b)
	}
	cp1252 := map[byte]rune{0x80: '€', 0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x96: '–', 0x97: '—', 0x85: '…'}
	r := make([]rune, len(b))
	for i, c := range b {
		if x, ok := cp1252[c]; ok {
			r[i] = x
		} else {
			r[i] = rune(c)
		}
	}
	return string(r)
}

func ipsLocales() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, ad := range addrs {
			if ipn, ok := ad.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

// servirLumbrera inicia el servidor (bloqueante).
func servirLumbrera(a *App) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.Puerto),
		Handler:           a.rutas(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logf("Lumbrera %s escuchando en el puerto %d", version, a.cfg.Puerto)
	return srv.ListenAndServe()
}
