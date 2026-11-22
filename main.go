package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed recursos/LEEME.txt
var leemeTxt []byte

//go:embed recursos/CHANGELOG.txt
var changelogTxt []byte

func mustSub(dir string) fs.FS {
	s, err := fs.Sub(webFS, dir)
	if err != nil {
		panic(err)
	}
	return s
}

func main() {
	modo := flag.String("modo", "", "lumbrera | atalaya | instalar | desinstalar | detener")
	dir := flag.String("dir", "", "carpeta base (por defecto, la del programa)")
	sinNav := flag.Bool("sin-navegador", false, "no abrir ventanas del navegador")
	flag.Parse()

	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	nombre := strings.ToLower(filepath.Base(exe))
	if *modo == "" {
		switch {
		case strings.Contains(nombre, "atalaya"):
			*modo = "atalaya"
		case strings.HasPrefix(nombre, "lumbrera"):
			*modo = "lumbrera"
		default:
			*modo = "instalar"
		}
	}
	baseDir = *dir
	if baseDir == "" {
		baseDir = filepath.Dir(exe)
	}

	switch *modo {
	case "instalar":
		instalar(exe)
	case "desinstalar":
		desinstalar()
	case "detener":
		detenerTodo()
	case "atalaya":
		correrAtalaya(!*sinNav)
	default:
		correrLumbrera(!*sinNav)
	}
}

func responde(url, app string) bool {
	cli := http.Client{Timeout: 800 * time.Millisecond}
	r, err := cli.Get(url + "/api/ping")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	var buf [200]byte
	n, _ := r.Body.Read(buf[:])
	return strings.Contains(string(buf[:n]), `"`+app+`"`)
}

func correrLumbrera(navegador bool) {
	dir := filepath.Join(baseDir, "lumbrera")
	iniciarLog(filepath.Join(dir, "lumbrera.log"))
	a, err := nuevaApp(dir)
	if err != nil {
		mostrarMensaje("Lumbrera", "No se pudo iniciar Lumbrera:\n\n"+err.Error())
		return
	}
	url := fmt.Sprintf("http://localhost:%d", a.cfg.Puerto)
	if responde(url, "lumbrera") {
		// ya está corriendo: solo abrir la consola
		if navegador {
			abrirURL(url + "/")
		}
		return
	}
	errc := make(chan error, 1)
	go func() { errc <- servirLumbrera(a) }()
	for i := 0; i < 50 && !responde(url, "lumbrera"); i++ {
		select {
		case err := <-errc:
			mostrarMensaje("Lumbrera", fmt.Sprintf("No se pudo abrir el puerto %d.\n\n%v\n\n¿Hay otro programa usando ese puerto? Puede cambiarlo en lumbrera\\config.json.", a.cfg.Puerto, err))
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if navegador {
		if a.cfg.AbrirProyector {
			if err := abrirProyector(a); err != nil {
				logf("proyector: %v", err)
			}
			time.Sleep(700 * time.Millisecond)
		}
		abrirURL(url + "/")
	}
	err = <-errc
	logf("servidor detenido: %v", err)
}

func correrAtalaya(navegador bool) {
	dir := filepath.Join(baseDir, "atalaya")
	iniciarLog(filepath.Join(dir, "atalaya.log"))
	t, err := nuevaAtalaya(dir)
	if err != nil {
		mostrarMensaje("Atalaya", "No se pudo iniciar Atalaya:\n\n"+err.Error())
		return
	}
	url := fmt.Sprintf("http://localhost:%d", t.cfg.Puerto)
	if responde(url, "atalaya") {
		if navegador {
			abrirURL(url + "/")
		}
		return
	}
	errc := make(chan error, 1)
	go func() { errc <- servirAtalaya(t) }()
	for i := 0; i < 50 && !responde(url, "atalaya"); i++ {
		select {
		case err := <-errc:
			mostrarMensaje("Atalaya", fmt.Sprintf("No se pudo abrir el puerto %d.\n\n%v\n\nPuede cambiarlo en atalaya\\config.json.", t.cfg.Puerto, err))
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if navegador {
		abrirURL(url + "/")
	}
	err = <-errc
	logf("Atalaya detenida: %v", err)
}

// prepararCarpeta deja la estructura y archivos iniciales (sin pisar datos existentes).
func prepararCarpeta(base string) error {
	for _, d := range []string{"lumbrera/datos/biblias", "lumbrera/datos/canciones", "lumbrera/datos/fondos", "atalaya"} {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	cl := filepath.Join(base, "lumbrera", "config.json")
	if _, err := os.Stat(cl); err != nil {
		escribirJSON(cl, configLumbreraPorDefecto())
	}
	ca := filepath.Join(base, "atalaya", "config.json")
	if _, err := os.Stat(ca); err != nil {
		escribirJSON(ca, configAtalayaPorDefecto())
	}
	asegurarBibliaIncluida(filepath.Join(base, "lumbrera", "datos", "biblias"))
	cancionesEjemplo(filepath.Join(base, "lumbrera", "datos", "canciones"))
	os.WriteFile(filepath.Join(base, "LEEME.txt"), []byte(strings.ReplaceAll(string(leemeTxt), "\n", "\r\n")), 0o644)
	os.WriteFile(filepath.Join(base, "CHANGELOG.txt"), []byte(strings.ReplaceAll(string(changelogTxt), "\n", "\r\n")), 0o644)
	return nil
}

func cancionesEjemplo(dir string) {
	ents, _ := os.ReadDir(dir)
	if len(ents) > 0 {
		return
	}
	c := nuevoCancionero(dir)
	c.guardar(Cancion{Titulo: "Ejemplo — cómo se arma una canción", Autor: "Lumbrera", Letra: `[Estrofa 1]
Cada bloque de líneas es una lámina
que se proyecta completa en pantalla.

[Coro]
Deje una línea en blanco
para pasar a la lámina siguiente.

[Estrofa 2]
Las etiquetas entre corchetes
como [Coro] o [Puente] son opcionales.

[Final]
Puede borrar esta canción de ejemplo
cuando cargue sus alabanzas.`})
}
