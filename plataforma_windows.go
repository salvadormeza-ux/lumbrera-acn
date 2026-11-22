//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW        = user32.NewProc("MessageBoxW")
	procEnumDisplayMon     = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
)

const (
	mbOK           = 0x0
	mbYesNo        = 0x4
	mbIconInfo     = 0x40
	mbIconQuestion = 0x20
	mbIconWarning  = 0x30
	mbTopmost      = 0x40000
	idYes          = 6
)

func caja(titulo, texto string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(titulo)
	m, _ := syscall.UTF16PtrFromString(texto)
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags|mbTopmost)
	return int(r)
}

func mostrarMensaje(titulo, texto string) { caja(titulo, texto, mbOK|mbIconInfo) }

func preguntar(titulo, texto string) bool {
	return caja(titulo, texto, mbYesNo|mbIconQuestion) == idYes
}

func comandoOculto(nombre string, args ...string) *exec.Cmd {
	c := exec.Command(nombre, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c
}

// Ojo: no usar comandoOculto para abrir ventanas; Windows pasaría SW_HIDE al navegador.
func abrirURL(u string) {
	exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

// abrirVentana abre una dirección como ventana de programa (sin pestañas ni barra),
// usando Edge o Chrome en modo aplicación con un perfil propio.
func abrirVentana(u, perfil string) {
	nav := buscarNavegador("")
	if nav == "" {
		abrirURL(u)
		return
	}
	args := []string{"--app=" + u, "--user-data-dir=" + filepath.Join(baseDir, perfil), "--no-first-run",
		"--no-default-browser-check", "--disable-features=Translate,msEdgeTranslate", "--start-maximized"}
	if err := exec.Command(nav, args...).Start(); err != nil {
		abrirURL(u)
	}
}

// ---------- pantallas ----------

type Pantalla struct {
	Num       int  `json:"num"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Ancho     int  `json:"ancho"`
	Alto      int  `json:"alto"`
	Principal bool `json:"principal"`
}

type rect struct{ L, T, R, B int32 }
type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

var (
	pantMu  sync.Mutex
	pantAcc []Pantalla
	pantCB  = syscall.NewCallback(func(h, hdc, lprc, data uintptr) uintptr {
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		procGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi)))
		pantAcc = append(pantAcc, Pantalla{len(pantAcc) + 1, int(mi.Monitor.L), int(mi.Monitor.T),
			int(mi.Monitor.R - mi.Monitor.L), int(mi.Monitor.B - mi.Monitor.T), mi.Flags&1 != 0})
		return 1
	})
)

func listarPantallas() []Pantalla {
	pantMu.Lock()
	defer pantMu.Unlock()
	procSetProcessDPIAware.Call()
	pantAcc = nil
	procEnumDisplayMon.Call(0, 0, pantCB, 0)
	return append([]Pantalla(nil), pantAcc...)
}

func buscarNavegador(pref string) string {
	cands := []string{pref,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func abrirProyector(a *App) error {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	nav := buscarNavegador(cfg.Navegador)
	url := fmt.Sprintf("http://localhost:%d/proyector", cfg.Puerto)
	if nav == "" {
		abrirURL(url)
		return errors.New("no se encontró Edge ni Chrome; se abrió el proyector en el navegador predeterminado")
	}
	ps := listarPantallas()
	var p *Pantalla
	if cfg.PantallaProyector > 0 && cfg.PantallaProyector <= len(ps) {
		p = &ps[cfg.PantallaProyector-1]
	} else {
		for i := range ps {
			if !ps[i].Principal {
				p = &ps[i]
				break
			}
		}
	}
	perfil := filepath.Join(baseDir, "lumbrera", "perfil-proyector")
	args := []string{"--app=" + url, "--user-data-dir=" + perfil, "--no-first-run", "--no-default-browser-check",
		"--disable-features=Translate,msEdgeTranslate", "--autoplay-policy=no-user-gesture-required", "--disable-session-crashed-bubble"}
	if p != nil {
		args = append(args, fmt.Sprintf("--window-position=%d,%d", p.X+40, p.Y+40), "--window-size=1024,640", "--start-fullscreen")
	} else {
		// una sola pantalla: ventana normal para no tapar la consola
		args = append(args, "--window-size=960,560")
	}
	return exec.Command(nav, args...).Start()
}

// ---------- detener / instalar ----------

func detenerTodo() {
	comandoOculto("taskkill", "/F", "/IM", "Atalaya.exe").Run()
	comandoOculto("taskkill", "/F", "/IM", "Lumbrera.exe").Run()
}

func carpetaInstalacion() string { return `C:\Lumbrera` }

func copiarArchivo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".nuevo"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()
	for i := 0; i < 20; i++ {
		os.Remove(dst)
		if err = os.Rename(tmp, dst); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

func powershell(script string) error {
	c := comandoOculto("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func crearAccesos(dir string) error {
	lum := filepath.Join(dir, "Lumbrera.exe")
	ata := filepath.Join(dir, "Atalaya.exe")
	script := `$ErrorActionPreference='Stop'
$sh = New-Object -ComObject WScript.Shell
$esc = [Environment]::GetFolderPath('Desktop')
$prog = Join-Path ([Environment]::GetFolderPath('Programs')) 'Lumbrera'
New-Item -ItemType Directory -Force -Path $prog | Out-Null
function Acceso($ruta, $destino, $argumentos, $desc) {
  $l = $sh.CreateShortcut($ruta); $l.TargetPath = $destino; $l.Arguments = $argumentos
  $l.WorkingDirectory = ` + psq(dir) + `; $l.IconLocation = $destino + ',0'; $l.Description = $desc; $l.Save()
}
Acceso (Join-Path $esc 'Lumbrera.lnk') ` + psq(lum) + ` '' 'Lumbrera - proyección de la iglesia'
Acceso (Join-Path $esc 'Atalaya.lnk') ` + psq(ata) + ` '' 'Atalaya - bot de transmisión (OBS)'
Acceso (Join-Path $prog 'Lumbrera.lnk') ` + psq(lum) + ` '' 'Lumbrera - proyección de la iglesia'
Acceso (Join-Path $prog 'Atalaya.lnk') ` + psq(ata) + ` '' 'Atalaya - bot de transmisión (OBS)'
Acceso (Join-Path $prog 'Detener Lumbrera.lnk') ` + psq(lum) + ` '--modo detener' 'Cierra Lumbrera y Atalaya'
Acceso (Join-Path $prog 'Desinstalar Lumbrera.lnk') ` + psq(lum) + ` '--modo desinstalar' 'Quita Lumbrera de este equipo'
Acceso (Join-Path $prog 'Carpeta de Lumbrera.lnk') ` + psq(dir) + ` '' 'Configuración, canciones y Biblias'
`
	return powershell(script)
}

func instalar(exe string) {
	dir := carpetaInstalacion()
	if !preguntar("Instalar Lumbrera "+version, "Se instalará Lumbrera "+version+" + Atalaya en "+dir+
		"\ncon accesos directos en el escritorio.\n\nSi ya estaba instalado, se actualiza el programa y se conservan\nlas canciones, Biblias, claves y ajustes.\n\n¿Continuar?") {
		return
	}
	detenerTodo()
	time.Sleep(800 * time.Millisecond)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		mostrarMensaje("Lumbrera", "No se pudo crear "+dir+":\n"+err.Error()+"\n\nPruebe ejecutar el instalador con clic derecho → Ejecutar como administrador.")
		return
	}
	baseDir = dir
	iniciarLog(filepath.Join(dir, "lumbrera", "instalacion.log"))
	for _, n := range []string{"Lumbrera.exe", "Atalaya.exe"} {
		if err := copiarArchivo(exe, filepath.Join(dir, n)); err != nil {
			mostrarMensaje("Lumbrera", "No se pudo copiar "+n+":\n"+err.Error())
			return
		}
	}
	if err := prepararCarpeta(dir); err != nil {
		mostrarMensaje("Lumbrera", "Error preparando la carpeta:\n"+err.Error())
		return
	}
	if err := crearAccesos(dir); err != nil {
		logf("accesos directos: %v", err)
		mostrarMensaje("Lumbrera", "Lumbrera quedó instalado en "+dir+", pero no se pudieron crear los accesos directos:\n"+err.Error()+
			"\n\nPuede abrir "+dir+`\Lumbrera.exe directamente.`)
	}
	logf("Instalado Lumbrera %s en %s", version, dir)
	if preguntar("Lumbrera instalado", "Lumbrera "+version+" quedó instalado en "+dir+".\n\n"+
		"Consola: http://localhost:8092  (clave inicial: acn2026)\n"+
		"Atalaya: http://localhost:8080  (clave inicial: acn2026)\n\n"+
		"Si Windows pregunta por el Firewall, marque «Redes privadas» y Permitir\n(para usar la consola desde el celular).\n\n¿Abrir Lumbrera ahora?") {
		c := exec.Command(filepath.Join(dir, "Lumbrera.exe"))
		c.Dir = dir
		c.Start()
	}
}

func desinstalar() {
	dir := baseDir
	if !preguntar("Desinstalar Lumbrera", "¿Quitar Lumbrera y Atalaya de este equipo?") {
		return
	}
	borrarDatos := preguntar("Desinstalar Lumbrera", "¿Borrar también las canciones, Biblias importadas y ajustes?\n\nSí = borrar todo\nNo = conservarlos en "+dir)
	comandoOculto("taskkill", "/F", "/IM", "Atalaya.exe").Run()
	powershell(`$esc=[Environment]::GetFolderPath('Desktop'); Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $esc 'Lumbrera.lnk'),(Join-Path $esc 'Atalaya.lnk'); Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path ([Environment]::GetFolderPath('Programs')) 'Lumbrera')`)
	var cmd string
	if borrarDatos {
		cmd = fmt.Sprintf(`ping 127.0.0.1 -n 3 > nul & rmdir /s /q "%s"`, dir)
	} else {
		cmd = fmt.Sprintf(`ping 127.0.0.1 -n 3 > nul & del /f /q "%s\Lumbrera.exe" "%s\Atalaya.exe"`, dir, dir)
	}
	mostrarMensaje("Lumbrera", "Lumbrera fue desinstalado.")
	c := comandoOculto("cmd", "/c", cmd)
	c.Start()
	comandoOculto("taskkill", "/F", "/IM", "Lumbrera.exe").Start()
}
