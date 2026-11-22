package main

// Importación de canciones: .txt (uno por canción, incluido lo exportado desde
// Holyrics), OpenLyrics .xml (OpenLP, OpenSong y otros) y .zip con muchos de ellos.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"html"
	"io"
	"path/filepath"
	"strings"
)

func cancionesDesdeArchivo(nombre string, data []byte) ([]Cancion, []string) {
	ext := strings.ToLower(filepath.Ext(nombre))
	base := strings.TrimSuffix(filepath.Base(nombre), filepath.Ext(nombre))
	switch {
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, []string{nombre + ": zip dañado"}
		}
		var out []Cancion
		var errs []string
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || f.UncompressedSize64 > 4<<20 || strings.HasPrefix(filepath.Base(f.Name), ".") || strings.Contains(f.Name, "__MACOSX") {
				continue
			}
			e := strings.ToLower(filepath.Ext(f.Name))
			if e != ".txt" && e != ".xml" && e != "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(io.LimitReader(rc, 4<<20))
			rc.Close()
			cs, es := cancionesDesdeArchivo(f.Name, b)
			out = append(out, cs...)
			errs = append(errs, es...)
		}
		return out, errs
	case ext == ".xml" || bytes.Contains(data[:min(len(data), 600)], []byte("<song")):
		c, err := desdeOpenLyrics(data)
		if err != nil {
			return nil, []string{nombre + ": " + err.Error()}
		}
		if c.Titulo == "" {
			c.Titulo = base
		}
		return []Cancion{c}, nil
	default:
		texto := decodificarTexto(data)
		c := cancionDesdeTexto(base, texto)
		// Si el archivo repite el título en la primera línea (como exporta Holyrics), se quita.
		lineas := strings.SplitN(c.Letra, "\n", 3)
		if len(lineas) >= 2 && strings.TrimSpace(lineas[1]) == "" &&
			sinAcentos(strings.TrimSpace(lineas[0])) == sinAcentos(c.Titulo) {
			c.Letra = strings.TrimSpace(strings.Join(lineas[2:], "\n"))
		}
		return []Cancion{c}, nil
	}
}

func desdeOpenLyrics(data []byte) (Cancion, error) {
	var s struct {
		Titles  []string `xml:"properties>titles>title"`
		Authors []string `xml:"properties>authors>author"`
		Key     string   `xml:"properties>key"`
		Verses  []struct {
			Name  string `xml:"name,attr"`
			Lines []struct {
				Inner string `xml:",innerxml"`
			} `xml:"lines"`
		} `xml:"lyrics>verse"`
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	if err := dec.Decode(&s); err != nil {
		return Cancion{}, err
	}
	c := Cancion{Tono: s.Key}
	if len(s.Titles) > 0 {
		c.Titulo = strings.TrimSpace(s.Titles[0])
	}
	c.Autor = strings.Join(s.Authors, ", ")
	nombres := map[byte]string{'v': "Estrofa", 'c': "Coro", 'b': "Puente", 'p': "Pre-coro", 'e': "Final", 'i': "Intro", 'o': "Final"}
	var bloques []string
	for _, v := range s.Verses {
		etq := ""
		if n := strings.ToLower(strings.TrimSpace(v.Name)); n != "" {
			if t, ok := nombres[n[0]]; ok {
				etq = strings.TrimSpace(t + " " + strings.TrimLeft(n[1:], "abcdefghijklmnopqrstuvwxyz"))
			}
		}
		for _, l := range v.Lines {
			t := strings.NewReplacer("<br/>", "\n", "<br />", "\n", "<br>", "\n").Replace(l.Inner)
			t = limpiarLineas(reEtiqueta.ReplaceAllString(t, ""))
			if t == "" {
				continue
			}
			if etq != "" {
				t = "[" + etq + "]\n" + t
				etq = ""
			}
			bloques = append(bloques, t)
		}
	}
	c.Letra = strings.Join(bloques, "\n\n")
	if c.Letra == "" {
		return c, errFormato("el XML no tiene estrofas (¿es OpenLyrics?)")
	}
	return c, nil
}

type errFormato string

func (e errFormato) Error() string { return string(e) }

func limpiarLineas(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(html.UnescapeString(l))
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
