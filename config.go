package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const version = "2.0.1"

var (
	baseDir    string // C:\Lumbrera
	logMu      sync.Mutex
	logArchivo *os.File
)

func iniciarLog(p string) {
	os.MkdirAll(filepath.Dir(p), 0o755)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 5<<20 {
		os.Rename(p, p+".anterior")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		logArchivo = f
	}
}

func logf(format string, a ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	msg := fmt.Sprintf(format, a...)
	if logArchivo != nil {
		fmt.Fprintf(logArchivo, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	}
	log.Println(msg)
}

// ---------- Lumbrera ----------

type Estilo struct {
	Fuente      string `json:"fuente"`
	TamanoMax   int    `json:"tamano_max"` // px a 1080p
	ColorTexto  string `json:"color_texto"`
	ColorFondo  string `json:"color_fondo"`
	ImagenFondo string `json:"imagen_fondo"` // nombre de archivo en datos/fondos
	Oscurecer   int    `json:"oscurecer"`    // 0-90 % sobre la imagen
	Sombra      bool   `json:"sombra"`
	Mayusculas  bool   `json:"mayusculas"`
	MostrarRef  bool   `json:"mostrar_referencia"`
	Alineacion  string `json:"alineacion"`
	Logo        string `json:"logo"`
	ColorAviso  string `json:"color_aviso"`
	OBSTamano   int    `json:"obs_tamano"`
	OBSFranja   bool   `json:"obs_franja"`
}

type ConfigLumbrera struct {
	Puerto            int    `json:"puerto"`
	Clave             string `json:"clave"`
	Iglesia           string `json:"iglesia"`
	BibliaPred        string `json:"biblia_predeterminada"`
	AbrirProyector    bool   `json:"abrir_proyector_al_iniciar"`
	PantallaProyector int    `json:"pantalla_proyector"` // 0 = automática (la 2ª)
	Navegador         string `json:"navegador"`          // ruta opcional a Edge/Chrome
	Estilo            Estilo `json:"estilo"`
	VersionConfig     int    `json:"version_config"`
}

func configLumbreraPorDefecto() ConfigLumbrera {
	return ConfigLumbrera{
		Puerto: 8092, Clave: "acn2026", Iglesia: "Iglesia ACN · Templo Central IFA", BibliaPred: "RV1909",
		AbrirProyector: true,
		Estilo: Estilo{Fuente: "Segoe UI", TamanoMax: 96, ColorTexto: "#ffffff", ColorFondo: "#0b1b2e",
			Oscurecer: 45, Sombra: true, MostrarRef: true, Alineacion: "center", ColorAviso: "#9bd3ff",
			OBSTamano: 54, OBSFranja: true, Logo: nombreLogoIglesia},
		VersionConfig: 2,
	}
}

// ---------- Atalaya ----------

type ConfigAtalaya struct {
	Puerto int    `json:"puerto"`
	Clave  string `json:"clave"`
	OBS    struct {
		Host     string `json:"host"`
		Puerto   int    `json:"puerto"`
		Password string `json:"password"`
	} `json:"obs"`
	LumbreraURL string `json:"lumbrera_url"`
}

func configAtalayaPorDefecto() ConfigAtalaya {
	c := ConfigAtalaya{Puerto: 8080, Clave: "acn2026", LumbreraURL: "http://localhost:8092"}
	c.OBS.Host = "127.0.0.1"
	c.OBS.Puerto = 4455
	return c
}

func leerJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func escribirJSON(p string, v any) error {
	os.MkdirAll(filepath.Dir(p), 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
