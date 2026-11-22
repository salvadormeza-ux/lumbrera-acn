//go:build !windows

package main

import (
	"fmt"
	"os/exec"
)

type Pantalla struct {
	Num       int  `json:"num"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Ancho     int  `json:"ancho"`
	Alto      int  `json:"alto"`
	Principal bool `json:"principal"`
}

func mostrarMensaje(titulo, texto string) { fmt.Printf("[%s] %s\n", titulo, texto) }
func preguntar(titulo, texto string) bool { fmt.Printf("[%s] %s (sí)\n", titulo, texto); return true }
func abrirURL(u string)                   { exec.Command("xdg-open", u).Start() }
func listarPantallas() []Pantalla         { return []Pantalla{{1, 0, 0, 1920, 1080, true}} }
func abrirProyector(a *App) error {
	abrirURL(fmt.Sprintf("http://localhost:%d/proyector", a.cfg.Puerto))
	return nil
}
func detenerTodo() {}
func desinstalar() {}
func instalar(exe string) {
	if err := prepararCarpeta(baseDir); err != nil {
		fmt.Println(err)
	}
}
