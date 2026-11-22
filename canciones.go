package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Cancion struct {
	ID         string   `json:"id"`
	Titulo     string   `json:"titulo"`
	Autor      string   `json:"autor"`
	Tono       string   `json:"tono"`
	Letra      string   `json:"letra"` // texto; línea en blanco = nueva lámina; [Coro] = etiqueta
	Modificada string   `json:"modificada"`
	Laminas    []Lamina `json:"laminas,omitempty"`
}

type CancionResumen struct {
	ID     string `json:"id"`
	Titulo string `json:"titulo"`
	Autor  string `json:"autor"`
	Inicio string `json:"inicio"`
}

type Cancionero struct {
	mu   sync.RWMutex
	dir  string
	todo map[string]*Cancion
}

func nuevoCancionero(dir string) *Cancionero {
	c := &Cancionero{dir: dir, todo: map[string]*Cancion{}}
	os.MkdirAll(dir, 0o755)
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var x Cancion
		if err := leerJSON(filepath.Join(dir, e.Name()), &x); err == nil && x.ID != "" {
			x.Laminas = nil
			c.todo[x.ID] = &x
		}
	}
	return c
}

var reEtiquetaCancion = regexp.MustCompile(`^\s*[\[(]?\s*((?i:coro|estrofa|verso|puente|pre-?coro|final|intro|outro|tag)[^\])]*)[\])]?\s*:?\s*$`)

func laminasDe(c *Cancion) []Lamina {
	texto := strings.ReplaceAll(c.Letra, "\r\n", "\n")
	texto = strings.ReplaceAll(texto, "\r", "\n")
	var out []Lamina
	for _, bloque := range regexp.MustCompile(`\n\s*\n`).Split(texto, -1) {
		lineas := strings.Split(strings.TrimSpace(bloque), "\n")
		etiqueta := ""
		if len(lineas) > 0 {
			if m := reEtiquetaCancion.FindStringSubmatch(lineas[0]); m != nil {
				etiqueta = strings.TrimSpace(m[1])
				lineas = lineas[1:]
			}
		}
		var limpias []string
		for _, l := range lineas {
			l = strings.TrimSpace(l)
			if l != "" {
				limpias = append(limpias, l)
			}
		}
		if len(limpias) == 0 {
			continue
		}
		out = append(out, Lamina{Texto: strings.Join(limpias, "\n"), Ref: c.Titulo, Etiqueta: etiqueta})
	}
	return out
}

func cancionDesdeTexto(titulo, texto string) Cancion {
	texto = strings.ReplaceAll(texto, "\r\n", "\n")
	lineas := strings.Split(texto, "\n")
	c := Cancion{Titulo: strings.TrimSpace(titulo)}
	// Encabezados opcionales: "Título: ...", "Autor: ...", "Tono: ..."
	i := 0
	for ; i < len(lineas) && i < 6; i++ {
		l := strings.TrimSpace(lineas[i])
		low := sinAcentos(l)
		switch {
		case strings.HasPrefix(low, "titulo:"):
			c.Titulo = strings.TrimSpace(l[strings.Index(l, ":")+1:])
		case strings.HasPrefix(low, "autor:"):
			c.Autor = strings.TrimSpace(l[strings.Index(l, ":")+1:])
		case strings.HasPrefix(low, "tono:"):
			c.Tono = strings.TrimSpace(l[strings.Index(l, ":")+1:])
		default:
			goto fin
		}
	}
fin:
	c.Letra = strings.TrimSpace(strings.Join(lineas[i:], "\n"))
	return c
}

func (c *Cancionero) guardar(x Cancion) (*Cancion, error) {
	x.Titulo = strings.TrimSpace(x.Titulo)
	x.Letra = strings.TrimSpace(x.Letra)
	if x.Titulo == "" {
		return nil, errors.New("la canción necesita un título")
	}
	if x.Letra == "" {
		return nil, errors.New("la canción no tiene letra")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if x.ID == "" || c.todo[x.ID] == nil {
		x.ID = nombreArchivoSeguro(sinAcentos(x.Titulo))
		if len(x.ID) > 50 {
			x.ID = x.ID[:50]
		}
		base := x.ID
		for n := 2; c.todo[x.ID] != nil; n++ {
			x.ID = base + "_" + itoa(n)
		}
	}
	x.ID = nombreArchivoSeguro(x.ID)
	x.Modificada = time.Now().Format("2006-01-02 15:04")
	x.Laminas = nil
	if err := escribirJSON(filepath.Join(c.dir, x.ID+".json"), x); err != nil {
		return nil, err
	}
	cp := x
	c.todo[x.ID] = &cp
	x.Laminas = laminasDe(&x)
	return &x, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func (c *Cancionero) obtener(id string) *Cancion {
	c.mu.RLock()
	defer c.mu.RUnlock()
	x, ok := c.todo[id]
	if !ok {
		return nil
	}
	cp := *x
	cp.Laminas = laminasDe(&cp)
	return &cp
}

func (c *Cancionero) borrar(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.todo[id]; !ok {
		return errors.New("no existe")
	}
	delete(c.todo, id)
	return os.Remove(filepath.Join(c.dir, nombreArchivoSeguro(id)+".json"))
}

func (c *Cancionero) buscar(q string) []CancionResumen {
	qn := sinAcentos(strings.TrimSpace(q))
	c.mu.RLock()
	defer c.mu.RUnlock()
	type punt struct {
		r CancionResumen
		p int
	}
	var res []punt
	for _, x := range c.todo {
		inicio := ""
		if ls := laminasDe(x); len(ls) > 0 {
			inicio = strings.SplitN(ls[0].Texto, "\n", 2)[0]
		}
		r := CancionResumen{x.ID, x.Titulo, x.Autor, inicio}
		if qn == "" {
			res = append(res, punt{r, 0})
			continue
		}
		t := sinAcentos(x.Titulo)
		switch {
		case strings.HasPrefix(t, qn):
			res = append(res, punt{r, 0})
		case strings.Contains(t, qn):
			res = append(res, punt{r, 1})
		case strings.Contains(sinAcentos(x.Letra), qn) || strings.Contains(sinAcentos(x.Autor), qn):
			res = append(res, punt{r, 2})
		}
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].p != res[j].p {
			return res[i].p < res[j].p
		}
		return sinAcentos(res[i].r.Titulo) < sinAcentos(res[j].r.Titulo)
	})
	out := make([]CancionResumen, 0, len(res))
	for _, p := range res {
		out = append(out, p.r)
	}
	return out
}
