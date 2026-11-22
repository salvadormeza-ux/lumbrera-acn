package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Biblia es el formato interno: libros 1..66 en orden canónico protestante.
type Biblia struct {
	Sigla  string      `json:"sigla"`
	Nombre string      `json:"nombre"`
	Libros []LibroData `json:"libros"` // índice 0 = Génesis
}

type LibroData struct {
	Capitulos [][]string `json:"c"` // [cap-1][vers-1]
}

type LibroInfo struct {
	Num     int      `json:"num"`
	Nombre  string   `json:"nombre"`
	Abrevs  []string `json:"-"`
	Corto   string   `json:"corto"`
	Testam  string   `json:"testamento"`
}

var libros = []LibroInfo{
	{1, "Génesis", []string{"gn", "gen", "ge"}, "Gn", "AT"},
	{2, "Éxodo", []string{"ex", "exo", "exod"}, "Ex", "AT"},
	{3, "Levítico", []string{"lv", "lev", "le"}, "Lv", "AT"},
	{4, "Números", []string{"nm", "num", "nu"}, "Nm", "AT"},
	{5, "Deuteronomio", []string{"dt", "deut", "deu", "de"}, "Dt", "AT"},
	{6, "Josué", []string{"jos", "josu"}, "Jos", "AT"},
	{7, "Jueces", []string{"jue", "jc", "jueces"}, "Jue", "AT"},
	{8, "Rut", []string{"rt", "rut"}, "Rt", "AT"},
	{9, "1 Samuel", []string{"1s", "1sa", "1sam"}, "1 S", "AT"},
	{10, "2 Samuel", []string{"2s", "2sa", "2sam"}, "2 S", "AT"},
	{11, "1 Reyes", []string{"1r", "1re", "1rey", "1ry"}, "1 R", "AT"},
	{12, "2 Reyes", []string{"2r", "2re", "2rey", "2ry"}, "2 R", "AT"},
	{13, "1 Crónicas", []string{"1cr", "1cro", "1cron"}, "1 Cr", "AT"},
	{14, "2 Crónicas", []string{"2cr", "2cro", "2cron"}, "2 Cr", "AT"},
	{15, "Esdras", []string{"esd", "esdr"}, "Esd", "AT"},
	{16, "Nehemías", []string{"neh", "ne"}, "Neh", "AT"},
	{17, "Ester", []string{"est", "es"}, "Est", "AT"},
	{18, "Job", []string{"job", "jb"}, "Job", "AT"},
	{19, "Salmos", []string{"sal", "sl", "salmo", "ps"}, "Sal", "AT"},
	{20, "Proverbios", []string{"pr", "pro", "prov"}, "Pr", "AT"},
	{21, "Eclesiastés", []string{"ec", "ecl", "ecles"}, "Ec", "AT"},
	{22, "Cantares", []string{"cnt", "cant", "ct", "cantar"}, "Cnt", "AT"},
	{23, "Isaías", []string{"is", "isa"}, "Is", "AT"},
	{24, "Jeremías", []string{"jr", "jer"}, "Jer", "AT"},
	{25, "Lamentaciones", []string{"lm", "lam"}, "Lm", "AT"},
	{26, "Ezequiel", []string{"ez", "eze", "ezeq"}, "Ez", "AT"},
	{27, "Daniel", []string{"dn", "dan", "da"}, "Dn", "AT"},
	{28, "Oseas", []string{"os", "ose"}, "Os", "AT"},
	{29, "Joel", []string{"jl", "joel"}, "Jl", "AT"},
	{30, "Amós", []string{"am", "amo"}, "Am", "AT"},
	{31, "Abdías", []string{"abd", "ab"}, "Abd", "AT"},
	{32, "Jonás", []string{"jon", "jns"}, "Jon", "AT"},
	{33, "Miqueas", []string{"mi", "miq"}, "Mi", "AT"},
	{34, "Nahúm", []string{"nah", "na"}, "Nah", "AT"},
	{35, "Habacuc", []string{"hab", "hb"}, "Hab", "AT"},
	{36, "Sofonías", []string{"sof", "so"}, "Sof", "AT"},
	{37, "Hageo", []string{"hag", "hg"}, "Hag", "AT"},
	{38, "Zacarías", []string{"zac", "za"}, "Zac", "AT"},
	{39, "Malaquías", []string{"mal", "ml"}, "Mal", "AT"},
	{40, "Mateo", []string{"mt", "mat"}, "Mt", "NT"},
	{41, "Marcos", []string{"mr", "mc", "mar", "marc"}, "Mr", "NT"},
	{42, "Lucas", []string{"lc", "luc", "lu"}, "Lc", "NT"},
	{43, "Juan", []string{"jn", "jua", "juan"}, "Jn", "NT"},
	{44, "Hechos", []string{"hch", "hech", "hec", "hch"}, "Hch", "NT"},
	{45, "Romanos", []string{"ro", "rom", "rm"}, "Ro", "NT"},
	{46, "1 Corintios", []string{"1co", "1cor"}, "1 Co", "NT"},
	{47, "2 Corintios", []string{"2co", "2cor"}, "2 Co", "NT"},
	{48, "Gálatas", []string{"ga", "gal"}, "Gá", "NT"},
	{49, "Efesios", []string{"ef", "efe"}, "Ef", "NT"},
	{50, "Filipenses", []string{"fil", "flp", "fi"}, "Fil", "NT"},
	{51, "Colosenses", []string{"col", "co"}, "Col", "NT"},
	{52, "1 Tesalonicenses", []string{"1ts", "1tes", "1te"}, "1 Ts", "NT"},
	{53, "2 Tesalonicenses", []string{"2ts", "2tes", "2te"}, "2 Ts", "NT"},
	{54, "1 Timoteo", []string{"1ti", "1tim"}, "1 Ti", "NT"},
	{55, "2 Timoteo", []string{"2ti", "2tim"}, "2 Ti", "NT"},
	{56, "Tito", []string{"tit", "tt"}, "Tit", "NT"},
	{57, "Filemón", []string{"flm", "filem", "fm"}, "Flm", "NT"},
	{58, "Hebreos", []string{"he", "heb"}, "He", "NT"},
	{59, "Santiago", []string{"stg", "sant", "san", "stgo"}, "Stg", "NT"},
	{60, "1 Pedro", []string{"1p", "1pe", "1ped"}, "1 P", "NT"},
	{61, "2 Pedro", []string{"2p", "2pe", "2ped"}, "2 P", "NT"},
	{62, "1 Juan", []string{"1jn", "1jua", "1juan"}, "1 Jn", "NT"},
	{63, "2 Juan", []string{"2jn", "2jua", "2juan"}, "2 Jn", "NT"},
	{64, "3 Juan", []string{"3jn", "3jua", "3juan"}, "3 Jn", "NT"},
	{65, "Judas", []string{"jud", "jds"}, "Jud", "NT"},
	{66, "Apocalipsis", []string{"ap", "apoc", "apo", "rev"}, "Ap", "NT"},
}

var quitaTildes = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "â", "a", "ê", "e", "î", "i", "ô", "o", "û", "u")

// sinAcentos pasa a minúsculas, quita tildes y espacios.
func sinAcentos(s string) string {
	return quitaTildes.Replace(strings.ToLower(s))
}

var reDeLibro = regexp.MustCompile(`^([123])(?:de|del|a|al|los|las)?(?:la|los|las)?`)

func claveLibro(s string) string {
	s = sinAcentos(s)
	s = strings.NewReplacer("primera", "1", "primer", "1", "segunda", "2", "segundo", "2", "tercera", "3", "tercer", "3",
		"iii ", "3", "ii ", "2", "i ", "1", " ", "", ".", "").Replace(s)
	s = reDeLibro.ReplaceAllString(s, "$1")
	return s
}

var indiceLibros map[string]int

func init() {
	indiceLibros = map[string]int{}
	for _, l := range libros {
		indiceLibros[claveLibro(l.Nombre)] = l.Num
		for _, a := range l.Abrevs {
			indiceLibros[a] = l.Num
		}
	}
	// nombres en inglés / alternativos frecuentes en archivos importados
	extra := map[string]int{"genesis": 1, "exodus": 2, "leviticus": 3, "numbers": 4, "deuteronomy": 5, "joshua": 6, "judges": 7, "ruth": 8,
		"1samuel": 9, "2samuel": 10, "1kings": 11, "2kings": 12, "1chronicles": 13, "2chronicles": 14, "ezra": 15, "nehemiah": 16, "esther": 17,
		"psalms": 19, "psalm": 19, "proverbs": 20, "ecclesiastes": 21, "songofsolomon": 22, "songofsongs": 22, "cantardeloscantares": 22, "isaiah": 23,
		"jeremiah": 24, "lamentations": 25, "ezekiel": 26, "hosea": 28, "amos": 30, "obadiah": 31, "jonah": 32, "micah": 33, "nahum": 34,
		"habakkuk": 35, "zephaniah": 36, "haggai": 37, "zechariah": 38, "malachi": 39, "matthew": 40, "mark": 41, "luke": 42, "john": 43,
		"acts": 44, "romans": 45, "1corinthians": 46, "2corinthians": 47, "galatians": 48, "ephesians": 49, "philippians": 50, "colossians": 51,
		"1thessalonians": 52, "2thessalonians": 53, "1timothy": 54, "2timothy": 55, "titus": 56, "philemon": 57, "hebrews": 58, "james": 59,
		"1peter": 60, "2peter": 61, "1john": 62, "2john": 63, "3john": 64, "jude": 65, "revelation": 66, "revelations": 66,
		"hechosdelosapostoles": 44, "apocalipsisdesanjuan": 66, "eclesiastes": 21, "cantar": 22, "sabiduria": -1, "sanmateo": 40, "sanmarcos": 41, "sanlucas": 42, "sanjuan": 43}
	for k, v := range extra {
		if v > 0 {
			indiceLibros[k] = v
		}
	}
}

// buscarLibro: coincidencia exacta o por prefijo único.
func buscarLibro(s string) int {
	k := claveLibro(s)
	if k == "" {
		return 0
	}
	if n, ok := indiceLibros[k]; ok {
		return n
	}
	cand := 0
	for _, l := range libros {
		if strings.HasPrefix(claveLibro(l.Nombre), k) {
			if cand != 0 && cand != l.Num {
				// ambiguo: preferir el primero en orden canónico
				return cand
			}
			cand = l.Num
		}
	}
	return cand
}

type Referencia struct {
	Libro     int `json:"libro"`
	Capitulo  int `json:"capitulo"`
	VerDesde  int `json:"desde"`
	VerHasta  int `json:"hasta"`
}

var reRef = regexp.MustCompile(`^\s*((?:[123]|i{1,3})?\s*\.?\s*[^\d:]+?)\s*(\d+)?(?:\s*[:.,\s]\s*(\d+)(?:\s*-\s*(\d+))?)?\s*$`)

func parsearReferencia(s string) (Referencia, bool) {
	s = strings.TrimSpace(s)
	m := reRef.FindStringSubmatch(sinAcentos(s))
	if m == nil {
		return Referencia{}, false
	}
	lib := buscarLibro(m[1])
	if lib == 0 {
		return Referencia{}, false
	}
	r := Referencia{Libro: lib, Capitulo: 1}
	if m[2] != "" {
		r.Capitulo, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		r.VerDesde, _ = strconv.Atoi(m[3])
		r.VerHasta = r.VerDesde
		if m[4] != "" {
			r.VerHasta, _ = strconv.Atoi(m[4])
		}
	}
	return r, true
}

// ---------- almacén de biblias ----------

type Biblioteca struct {
	mu      sync.RWMutex
	dir     string
	biblias map[string]*Biblia
}

func nuevaBiblioteca(dir string) *Biblioteca {
	b := &Biblioteca{dir: dir, biblias: map[string]*Biblia{}}
	b.cargar()
	return b
}

func (b *Biblioteca) cargar() {
	os.MkdirAll(b.dir, 0o755)
	ents, _ := os.ReadDir(b.dir)
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json.gz") {
			continue
		}
		bb, err := leerBibliaGz(filepath.Join(b.dir, e.Name()))
		if err != nil {
			logf("no se pudo leer biblia %s: %v", e.Name(), err)
			continue
		}
		b.biblias[bb.Sigla] = bb
	}
}

func leerBibliaGz(p string) (*Biblia, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var bb Biblia
	if err := json.NewDecoder(gz).Decode(&bb); err != nil {
		return nil, err
	}
	for len(bb.Libros) < 66 {
		bb.Libros = append(bb.Libros, LibroData{})
	}
	return &bb, nil
}

func (b *Biblioteca) guardar(bb *Biblia) error {
	os.MkdirAll(b.dir, 0o755)
	p := filepath.Join(b.dir, nombreArchivoSeguro(bb.Sigla)+".json.gz")
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	if err := json.NewEncoder(gz).Encode(bb); err != nil {
		f.Close()
		return err
	}
	gz.Close()
	f.Close()
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	b.mu.Lock()
	b.biblias[bb.Sigla] = bb
	b.mu.Unlock()
	return nil
}

func (b *Biblioteca) borrar(sigla string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.biblias[sigla]; !ok {
		return fmt.Errorf("no existe")
	}
	delete(b.biblias, sigla)
	return os.Remove(filepath.Join(b.dir, nombreArchivoSeguro(sigla)+".json.gz"))
}

func (b *Biblioteca) obtener(sigla string) *Biblia {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if bb, ok := b.biblias[sigla]; ok {
		return bb
	}
	return nil
}

type BibliaResumen struct {
	Sigla     string `json:"sigla"`
	Nombre    string `json:"nombre"`
	Versiculos int   `json:"versiculos"`
}

func (b *Biblioteca) lista() []BibliaResumen {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []BibliaResumen
	for _, bb := range b.biblias {
		n := 0
		for _, l := range bb.Libros {
			for _, c := range l.Capitulos {
				n += len(c)
			}
		}
		out = append(out, BibliaResumen{bb.Sigla, bb.Nombre, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sigla < out[j].Sigla })
	return out
}

type ResultadoBusqueda struct {
	Libro    int    `json:"libro"`
	Capitulo int    `json:"capitulo"`
	Versiculo int   `json:"versiculo"`
	Ref      string `json:"ref"`
	Texto    string `json:"texto"`
}

func (bb *Biblia) buscarTexto(q string, max int) []ResultadoBusqueda {
	qn := sinAcentos(strings.TrimSpace(q))
	if len([]rune(qn)) < 3 {
		return nil
	}
	palabras := strings.Fields(qn)
	var out []ResultadoBusqueda
	for li, l := range bb.Libros {
		for ci, c := range l.Capitulos {
			for vi, v := range c {
				vn := sinAcentos(v)
				ok := true
				for _, p := range palabras {
					if !strings.Contains(vn, p) {
						ok = false
						break
					}
				}
				if ok {
					out = append(out, ResultadoBusqueda{li + 1, ci + 1, vi + 1,
						fmt.Sprintf("%s %d:%d", libros[li].Nombre, ci+1, vi+1), v})
					if len(out) >= max {
						return out
					}
				}
			}
		}
	}
	return out
}

func nombreArchivoSeguro(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "biblia"
	}
	return b.String()
}
