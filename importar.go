package main

// Importación de Biblias: Zefania XML, OpenSong / OpenLP XML, OSIS, MyBible,
// e-Sword (.bblx), CSV/TSV, JSON y .zip que contenga cualquiera de ellos.

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type acumulador struct {
	libros  [66]map[int]map[int]string
	titulo  string
	formato string
	n       int
}

func (a *acumulador) poner(lib, cap, ver int, txt string) {
	if lib < 1 || lib > 66 || cap < 1 || ver < 1 || cap > 200 || ver > 250 {
		return
	}
	txt = limpiarTexto(txt)
	if txt == "" {
		return
	}
	if a.libros[lib-1] == nil {
		a.libros[lib-1] = map[int]map[int]string{}
	}
	if a.libros[lib-1][cap] == nil {
		a.libros[lib-1][cap] = map[int]string{}
	}
	if prev, ok := a.libros[lib-1][cap][ver]; ok {
		txt = prev + " " + txt
	} else {
		a.n++
	}
	a.libros[lib-1][cap][ver] = txt
}

func (a *acumulador) biblia(sigla, nombre string) *Biblia {
	bb := &Biblia{Sigla: sigla, Nombre: nombre, Libros: make([]LibroData, 66)}
	for i, caps := range a.libros {
		maxc := 0
		for c := range caps {
			if c > maxc {
				maxc = c
			}
		}
		bb.Libros[i].Capitulos = make([][]string, maxc)
		for c, vs := range caps {
			maxv := 0
			for v := range vs {
				if v > maxv {
					maxv = v
				}
			}
			arr := make([]string, maxv)
			for v, t := range vs {
				arr[v-1] = t
			}
			bb.Libros[i].Capitulos[c-1] = arr
		}
	}
	return bb
}

var (
	reEtiquetasNota = regexp.MustCompile(`(?is)<(S|f|n|note|NOTE|rf|RF|gr|GR|STYLE[^>]*css="display:none"[^>]*)>.*?</(S|f|n|note|NOTE|rf|RF|gr|GR|STYLE)>`)
	reEtiqueta      = regexp.MustCompile(`<[^>]*>`)
	reRTFCtrl       = regexp.MustCompile(`\\[a-zA-Z]+-?\d* ?`)
	reEspacios      = regexp.MustCompile(`\s+`)
)

func limpiarTexto(s string) string {
	if strings.Contains(s, "<") {
		s = reEtiquetasNota.ReplaceAllString(s, "")
		s = reEtiqueta.ReplaceAllString(s, " ")
	}
	if strings.Contains(s, `\`) {
		s = reRTFCtrl.ReplaceAllString(s, "")
		s = strings.NewReplacer("{", "", "}", "", `\`, "").Replace(s)
	}
	s = html.UnescapeString(s)
	s = reEspacios.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, " ,", ",")
	s = strings.ReplaceAll(s, " .", ".")
	s = strings.ReplaceAll(s, " ;", ";")
	s = strings.ReplaceAll(s, " :", ":")
	return strings.TrimSpace(s)
}

// importarBiblia detecta el formato e interpreta el contenido.
func importarBiblia(nombreArchivo string, data []byte) (*acumulador, error) {
	if len(data) == 0 {
		return nil, errors.New("archivo vacío")
	}
	// zip
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("zip dañado: %v", err)
		}
		fs := append([]*zip.File(nil), zr.File...)
		sort.Slice(fs, func(i, j int) bool { return fs[i].UncompressedSize64 > fs[j].UncompressedSize64 })
		var ultimoErr error
		for _, f := range fs {
			if f.FileInfo().IsDir() || f.UncompressedSize64 > 300<<20 {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(rc, 300<<20))
			rc.Close()
			if err != nil {
				continue
			}
			a, err := importarBiblia(f.Name, b)
			if err == nil && a.n > 0 {
				return a, nil
			}
			ultimoErr = err
		}
		if ultimoErr == nil {
			ultimoErr = errors.New("el zip no contiene una Biblia reconocible")
		}
		return nil, ultimoErr
	}
	if bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
		return importarSQLite(data)
	}
	// quitar BOM
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	ini := bytes.TrimSpace(data[:min(len(data), 4096)])
	switch {
	case bytes.HasPrefix(ini, []byte("<")):
		return importarXML(data)
	case bytes.HasPrefix(ini, []byte("{")) || bytes.HasPrefix(ini, []byte("[")):
		return importarJSON(data)
	default:
		return importarCSV(data)
	}
}

// ---------- SQLite: MyBible y e-Sword ----------

var myBibleNums = map[int]int{10: 1, 20: 2, 30: 3, 40: 4, 50: 5, 60: 6, 70: 7, 80: 8, 90: 9, 100: 10, 110: 11, 120: 12, 130: 13, 140: 14,
	150: 15, 160: 16, 190: 17, 220: 18, 230: 19, 240: 20, 250: 21, 260: 22, 290: 23, 300: 24, 310: 25, 330: 26, 340: 27, 350: 28, 360: 29,
	370: 30, 380: 31, 390: 32, 400: 33, 410: 34, 420: 35, 430: 36, 440: 37, 450: 38, 460: 39, 470: 40, 480: 41, 490: 42, 500: 43, 510: 44,
	520: 59, 530: 60, 540: 61, 550: 62, 560: 63, 570: 64, 580: 65, 590: 45, 600: 46, 610: 47, 620: 48, 630: 49, 640: 50, 650: 51, 660: 52,
	670: 53, 680: 54, 690: 55, 700: 56, 710: 57, 720: 58, 730: 66}

func importarSQLite(data []byte) (*acumulador, error) {
	db, err := abrirSQLite(data)
	if err != nil {
		return nil, err
	}
	a := &acumulador{}
	switch {
	case db.tieneTabla("verses"): // MyBible
		a.formato = "MyBible"
		filas, err := db.leerTabla("verses")
		if err != nil {
			return nil, err
		}
		for _, f := range filas {
			lib := myBibleNums[comoInt(f["book_number"])]
			a.poner(lib, comoInt(f["chapter"]), comoInt(f["verse"]), comoTexto(f["text"]))
		}
		if db.tieneTabla("info") {
			if info, err := db.leerTabla("info"); err == nil {
				for _, f := range info {
					if strings.EqualFold(comoTexto(f["name"]), "description") {
						a.titulo = comoTexto(f["value"])
					}
				}
			}
		}
	case db.tieneTabla("bible"): // e-Sword
		a.formato = "e-Sword"
		filas, err := db.leerTabla("bible")
		if err != nil {
			return nil, err
		}
		for _, f := range filas {
			a.poner(comoInt(f["book"]), comoInt(f["chapter"]), comoInt(f["verse"]), comoTexto(f["scripture"]))
		}
		if db.tieneTabla("details") {
			if det, err := db.leerTabla("details"); err == nil && len(det) > 0 {
				a.titulo = comoTexto(det[0]["title"])
				if a.titulo == "" {
					a.titulo = comoTexto(det[0]["description"])
				}
			}
		}
	default:
		return nil, errors.New("SQLite sin tabla de versículos conocida (MyBible: verses, e-Sword: Bible)")
	}
	if a.n == 0 {
		return nil, errors.New("no se encontraron versículos")
	}
	return a, nil
}

// ---------- XML: Zefania, OpenSong/OpenLP, Beblia, OSIS ----------

var osisLibros = map[string]int{}

func init() {
	ab := strings.Fields("Gen Exod Lev Num Deut Josh Judg Ruth 1Sam 2Sam 1Kgs 2Kgs 1Chr 2Chr Ezra Neh Esth Job Ps Prov Eccl Song Isa Jer Lam Ezek Dan Hos Joel Amos Obad Jonah Mic Nah Hab Zeph Hag Zech Mal Matt Mark Luke John Acts Rom 1Cor 2Cor Gal Eph Phil Col 1Thess 2Thess 1Tim 2Tim Titus Phlm Heb Jas 1Pet 2Pet 1John 2John 3John Jude Rev")
	for i, x := range ab {
		osisLibros[strings.ToLower(x)] = i + 1
	}
}

func atributo(e xml.StartElement, nombres ...string) string {
	for _, n := range nombres {
		for _, at := range e.Attr {
			if strings.EqualFold(at.Name.Local, n) {
				return at.Value
			}
		}
	}
	return ""
}

func libroDesdeTexto(s string, sec int) int {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 66 {
		return n
	}
	if n, ok := osisLibros[strings.ToLower(s)]; ok {
		return n
	}
	if n := buscarLibro(s); n > 0 {
		return n
	}
	return sec
}

// parseOsisID "Gen.1.1" -> libro, cap, ver
func parseOsisID(id string) (int, int, int) {
	id = strings.Fields(id + " ")[0]
	p := strings.Split(id, ".")
	if len(p) < 3 {
		return 0, 0, 0
	}
	lib := osisLibros[strings.ToLower(p[len(p)-3])]
	c, _ := strconv.Atoi(p[len(p)-2])
	v, _ := strconv.Atoi(p[len(p)-1])
	return lib, c, v
}

func esNombre(n string, opciones ...string) bool {
	for _, o := range opciones {
		if strings.EqualFold(n, o) {
			return true
		}
	}
	return false
}

func importarXML(data []byte) (*acumulador, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(cs string, in io.Reader) (io.Reader, error) {
		cs = strings.ToLower(cs)
		if strings.Contains(cs, "8859") || strings.Contains(cs, "1252") || strings.Contains(cs, "latin") {
			b, _ := io.ReadAll(in)
			r := make([]rune, len(b))
			for i, c := range b {
				r[i] = rune(c)
			}
			return strings.NewReader(string(r)), nil
		}
		return in, nil
	}
	a := &acumulador{formato: "XML"}
	lib, cap, secLib := 0, 0, 0
	var verso struct {
		activo      bool
		lib, c, v   int
		buf         strings.Builder
		milestone   bool
	}
	ignorar := 0
	enTitulo := false
	var titulo strings.Builder
	cerrarVerso := func() {
		if verso.activo {
			a.poner(verso.lib, verso.c, verso.v, verso.buf.String())
		}
		verso.activo = false
		verso.buf.Reset()
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if a.n > 0 {
				break
			}
			return nil, fmt.Errorf("XML no válido: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := t.Name.Local
			switch {
			case ignorar > 0:
				ignorar++
			case esNombre(n, "note", "rf", "gr", "footnote", "fn", "title") && verso.activo:
				ignorar = 1
			case esNombre(n, "osis", "osistext"):
				a.formato = "OSIS"
			case esNombre(n, "xmlbible"):
				a.formato = "Zefania"
			case esNombre(n, "title", "biblename", "description") && a.titulo == "" && !verso.activo:
				enTitulo = true
			case esNombre(n, "biblebook", "book", "b") && atributo(t, "osisID") == "":
				secLib++
				nm := atributo(t, "bnumber", "number", "n", "bname", "name", "id")
				lib = libroDesdeTexto(nm, secLib)
				if b2 := atributo(t, "bnumber", "number"); b2 != "" {
					lib = libroDesdeTexto(b2, secLib)
				}
				cap = 0
			case esNombre(n, "div") && strings.EqualFold(atributo(t, "type"), "book"):
				if l, ok := osisLibros[strings.ToLower(atributo(t, "osisID"))]; ok {
					lib = l
				}
			case esNombre(n, "chapter", "c", "cap"):
				if id := atributo(t, "osisID", "sID"); id != "" {
					p := strings.Split(id, ".")
					if len(p) >= 2 {
						lib = osisLibros[strings.ToLower(p[len(p)-2])]
						cap, _ = strconv.Atoi(p[len(p)-1])
					}
				} else {
					c, err := strconv.Atoi(strings.TrimSpace(atributo(t, "cnumber", "number", "n", "id")))
					if err != nil {
						c = cap + 1
					}
					cap = c
				}
			case esNombre(n, "vers", "verse", "v"):
				if eid := atributo(t, "eID"); eid != "" {
					cerrarVerso()
					continue
				}
				cerrarVerso()
				vl, vc, vv := lib, cap, 0
				if id := atributo(t, "osisID", "sID"); id != "" && strings.Contains(id, ".") {
					vl, vc, vv = parseOsisID(id)
					verso.milestone = atributo(t, "sID") != ""
				} else {
					num := strings.TrimSpace(atributo(t, "vnumber", "number", "n", "id"))
					if i := strings.IndexAny(num, "-,"); i > 0 {
						num = num[:i]
					}
					vv, _ = strconv.Atoi(num)
					verso.milestone = false
				}
				verso.activo, verso.lib, verso.c, verso.v = true, vl, vc, vv
			}
		case xml.EndElement:
			n := t.Name.Local
			if ignorar > 0 {
				ignorar--
				continue
			}
			if enTitulo {
				enTitulo = false
				if a.titulo == "" {
					a.titulo = strings.TrimSpace(titulo.String())
				}
			}
			if esNombre(n, "vers", "verse", "v") && !verso.milestone {
				cerrarVerso()
			}
			if esNombre(n, "chapter", "c", "biblebook", "book", "b") && verso.milestone {
				cerrarVerso()
			}
		case xml.CharData:
			if ignorar > 0 {
				continue
			}
			if enTitulo {
				titulo.Write(t)
			}
			if verso.activo {
				verso.buf.Write(t)
				verso.buf.WriteByte(' ')
			}
		}
	}
	cerrarVerso()
	if a.n == 0 {
		return nil, errors.New("no se encontraron versículos en el XML")
	}
	if len(a.titulo) > 80 {
		a.titulo = a.titulo[:80]
	}
	return a, nil
}

// ---------- JSON ----------

func importarJSON(data []byte) (*acumulador, error) {
	a := &acumulador{formato: "JSON"}
	// formato propio de Lumbrera
	var propia Biblia
	if json.Unmarshal(data, &propia) == nil && len(propia.Libros) > 0 {
		for li, l := range propia.Libros {
			for ci, c := range l.Capitulos {
				for vi, v := range c {
					a.poner(li+1, ci+1, vi+1, v)
				}
			}
		}
		a.titulo = propia.Nombre
		if a.n > 0 {
			return a, nil
		}
	}
	// {translation, books:[{name, chapters:[{chapter, verses:[{verse,text}]}]}]}
	var sm struct {
		Translation string `json:"translation"`
		Books       []struct {
			Name     string `json:"name"`
			Chapters []struct {
				Chapter int `json:"chapter"`
				Verses  []struct {
					Verse int    `json:"verse"`
					Text  string `json:"text"`
				} `json:"verses"`
			} `json:"chapters"`
		} `json:"books"`
	}
	if json.Unmarshal(data, &sm) == nil && len(sm.Books) > 0 {
		for i, b := range sm.Books {
			lib := libroDesdeTexto(b.Name, i+1)
			for _, c := range b.Chapters {
				for _, v := range c.Verses {
					a.poner(lib, c.Chapter, v.Verse, v.Text)
				}
			}
		}
		a.titulo = sm.Translation
		if a.n > 0 {
			return a, nil
		}
	}
	// [{abbrev|name, chapters:[["v1","v2"],...]}]
	var arr []struct {
		Abbrev   string     `json:"abbrev"`
		Name     string     `json:"name"`
		Chapters [][]string `json:"chapters"`
	}
	if json.Unmarshal(data, &arr) == nil && len(arr) > 0 {
		for i, b := range arr {
			lib := i + 1
			for ci, c := range b.Chapters {
				for vi, v := range c {
					a.poner(lib, ci+1, vi+1, v)
				}
			}
		}
		if a.n > 0 {
			return a, nil
		}
	}
	return nil, errors.New("JSON con estructura no reconocida")
}

// ---------- CSV / TSV ----------

func importarCSV(data []byte) (*acumulador, error) {
	muestra := string(data[:min(len(data), 20000)])
	sep := ','
	mejor := 0
	for _, s := range []rune{'\t', ',', ';', '|'} {
		if c := strings.Count(muestra, string(s)); c > mejor {
			mejor, sep = c, s
		}
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = sep
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	a := &acumulador{formato: "CSV"}
	col := -1
	esNum := func(s string) bool { _, err := strconv.Atoi(strings.TrimSpace(s)); return err == nil }
	for {
		f, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if col < 0 {
			for i := 0; i+3 < len(f)+0 && i+3 <= len(f)-1; i++ {
				if esNum(f[i+1]) && esNum(f[i+2]) && libroDesdeTexto(f[i], 0) > 0 && !esNum(f[i+3]) {
					col = i
					break
				}
			}
			if col < 0 {
				continue
			}
		}
		if len(f) < col+4 {
			continue
		}
		lib := libroDesdeTexto(f[col], 0)
		c, _ := strconv.Atoi(strings.TrimSpace(f[col+1]))
		v, _ := strconv.Atoi(strings.TrimSpace(f[col+2]))
		a.poner(lib, c, v, strings.Join(f[col+3:], string(sep)))
	}
	if a.n == 0 {
		return nil, errors.New("formato no reconocido: use Zefania/OpenSong/OSIS XML, MyBible, e-Sword, CSV (libro,capítulo,versículo,texto), JSON o .zip")
	}
	return a, nil
}
