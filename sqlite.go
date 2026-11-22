package main

// Lector mínimo de solo lectura para archivos SQLite (MyBible, e-Sword).
// Recorre tablas con rowid; suficiente para importar Biblias.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

type sqliteDB struct {
	d        []byte
	pageSize int
	usable   int
	enc      int
}

func abrirSQLite(d []byte) (*sqliteDB, error) {
	if len(d) < 100 || string(d[:16]) != "SQLite format 3\x00" {
		return nil, errors.New("no es un archivo SQLite")
	}
	ps := int(binary.BigEndian.Uint16(d[16:18]))
	if ps == 1 {
		ps = 65536
	}
	db := &sqliteDB{d: d, pageSize: ps, usable: ps - int(d[20]), enc: int(binary.BigEndian.Uint32(d[56:60]))}
	if db.enc == 0 {
		db.enc = 1
	}
	return db, nil
}

func (db *sqliteDB) pagina(n int) ([]byte, error) {
	ini := (n - 1) * db.pageSize
	if n < 1 || ini+db.pageSize > len(db.d) {
		return nil, fmt.Errorf("página %d fuera de rango", n)
	}
	return db.d[ini : ini+db.pageSize], nil
}

func leerVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return (v << 8) | uint64(b[i]), 9
		}
		v = (v << 7) | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return v, len(b)
}

type filaSQL struct {
	rowid int64
	vals  []any
}

// recorrer visita cada fila de la tabla con raíz en root.
func (db *sqliteDB) recorrer(root int, fn func(filaSQL), prof int) error {
	if prof > 64 {
		return errors.New("árbol demasiado profundo")
	}
	pg, err := db.pagina(root)
	if err != nil {
		return err
	}
	h := 0
	if root == 1 {
		h = 100
	}
	tipo := pg[h]
	ncel := int(binary.BigEndian.Uint16(pg[h+3:]))
	switch tipo {
	case 0x05: // interior de tabla
		for i := 0; i < ncel; i++ {
			off := int(binary.BigEndian.Uint16(pg[h+12+2*i:]))
			hijo := int(binary.BigEndian.Uint32(pg[off:]))
			if err := db.recorrer(hijo, fn, prof+1); err != nil {
				return err
			}
		}
		der := int(binary.BigEndian.Uint32(pg[h+8:]))
		return db.recorrer(der, fn, prof+1)
	case 0x0D: // hoja de tabla
		for i := 0; i < ncel; i++ {
			off := int(binary.BigEndian.Uint16(pg[h+8+2*i:]))
			psize, n1 := leerVarint(pg[off:])
			rowid, n2 := leerVarint(pg[off+n1:])
			ini := off + n1 + n2
			payload, err := db.payload(pg, ini, int(psize))
			if err != nil {
				return err
			}
			vals, err := db.registro(payload)
			if err != nil {
				return err
			}
			fn(filaSQL{int64(rowid), vals})
		}
		return nil
	default:
		return fmt.Errorf("tipo de página no soportado (%x)", tipo)
	}
}

func (db *sqliteDB) payload(pg []byte, ini, p int) ([]byte, error) {
	u := db.usable
	x := u - 35
	if p <= x {
		return pg[ini : ini+p], nil
	}
	m := ((u-12)*32/255 - 23)
	k := m + ((p - m) % (u - 4))
	local := m
	if k <= x {
		local = k
	}
	out := make([]byte, 0, p)
	out = append(out, pg[ini:ini+local]...)
	sig := int(binary.BigEndian.Uint32(pg[ini+local:]))
	for len(out) < p && sig != 0 {
		op, err := db.pagina(sig)
		if err != nil {
			return nil, err
		}
		sig = int(binary.BigEndian.Uint32(op[0:4]))
		n := p - len(out)
		if n > u-4 {
			n = u - 4
		}
		out = append(out, op[4:4+n]...)
	}
	return out, nil
}

func (db *sqliteDB) texto(b []byte) string {
	switch db.enc {
	case 2, 3:
		u := make([]uint16, len(b)/2)
		for i := range u {
			if db.enc == 2 {
				u[i] = binary.LittleEndian.Uint16(b[2*i:])
			} else {
				u[i] = binary.BigEndian.Uint16(b[2*i:])
			}
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

func (db *sqliteDB) registro(p []byte) ([]any, error) {
	hs, n := leerVarint(p)
	if int(hs) > len(p) {
		return nil, errors.New("registro dañado")
	}
	var tipos []uint64
	for i := n; i < int(hs); {
		t, m := leerVarint(p[i:])
		tipos = append(tipos, t)
		i += m
	}
	pos := int(hs)
	vals := make([]any, len(tipos))
	entero := func(nb int) int64 {
		var v int64
		for i := 0; i < nb; i++ {
			v = v<<8 | int64(p[pos+i])
		}
		// extender signo
		sh := uint(64 - 8*nb)
		return v << sh >> sh
	}
	for i, t := range tipos {
		switch {
		case t == 0:
			vals[i] = nil
		case t >= 1 && t <= 6:
			nb := []int{0, 1, 2, 3, 4, 6, 8}[t]
			if pos+nb > len(p) {
				return nil, errors.New("registro corto")
			}
			vals[i] = entero(nb)
			pos += nb
		case t == 7:
			vals[i] = math.Float64frombits(binary.BigEndian.Uint64(p[pos:]))
			pos += 8
		case t == 8:
			vals[i] = int64(0)
		case t == 9:
			vals[i] = int64(1)
		case t >= 12:
			l := int(t-12) / 2
			if pos+l > len(p) {
				return nil, errors.New("registro corto")
			}
			if t%2 == 1 {
				vals[i] = db.texto(p[pos : pos+l])
			} else {
				vals[i] = append([]byte(nil), p[pos:pos+l]...)
			}
			pos += l
		}
	}
	return vals, nil
}

type tablaSQL struct {
	nombre   string
	root     int
	columnas []string
	aliasRow int // índice de la columna INTEGER PRIMARY KEY, -1 si no hay
}

func (db *sqliteDB) tablas() ([]tablaSQL, error) {
	var out []tablaSQL
	err := db.recorrer(1, func(f filaSQL) {
		if len(f.vals) < 5 {
			return
		}
		if t, _ := f.vals[0].(string); t != "table" {
			return
		}
		nom, _ := f.vals[1].(string)
		root, _ := f.vals[3].(int64)
		sql, _ := f.vals[4].(string)
		cols, alias := columnasDeSQL(sql)
		out = append(out, tablaSQL{nom, int(root), cols, alias})
	}, 0)
	return out, err
}

func columnasDeSQL(sql string) ([]string, int) {
	a := strings.Index(sql, "(")
	z := strings.LastIndex(sql, ")")
	if a < 0 || z <= a {
		return nil, -1
	}
	cuerpo := sql[a+1 : z]
	var partes []string
	nivel, ini := 0, 0
	for i, r := range cuerpo {
		switch r {
		case '(':
			nivel++
		case ')':
			nivel--
		case ',':
			if nivel == 0 {
				partes = append(partes, cuerpo[ini:i])
				ini = i + 1
			}
		}
	}
	partes = append(partes, cuerpo[ini:])
	var cols []string
	alias := -1
	for _, p := range partes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		up := strings.ToUpper(p)
		if strings.HasPrefix(up, "PRIMARY KEY") || strings.HasPrefix(up, "UNIQUE") || strings.HasPrefix(up, "CONSTRAINT") ||
			strings.HasPrefix(up, "CHECK") || strings.HasPrefix(up, "FOREIGN KEY") {
			continue
		}
		campos := strings.Fields(p)
		nom := strings.Trim(campos[0], "`\"[]'")
		if strings.Contains(strings.Join(strings.Fields(up), " "), "INTEGER PRIMARY KEY") {
			alias = len(cols)
		}
		cols = append(cols, strings.ToLower(nom))
	}
	return cols, alias
}

// leerTabla devuelve filas como mapas columna->valor.
func (db *sqliteDB) leerTabla(nombre string) ([]map[string]any, error) {
	ts, err := db.tablas()
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if !strings.EqualFold(t.nombre, nombre) {
			continue
		}
		var out []map[string]any
		err := db.recorrer(t.root, func(f filaSQL) {
			m := map[string]any{}
			for i, c := range t.columnas {
				if i == t.aliasRow {
					m[c] = f.rowid
					continue
				}
				if i < len(f.vals) {
					m[c] = f.vals[i]
				}
			}
			out = append(out, m)
		}, 0)
		return out, err
	}
	return nil, fmt.Errorf("no se encontró la tabla %q", nombre)
}

func (db *sqliteDB) tieneTabla(nombre string) bool {
	ts, _ := db.tablas()
	for _, t := range ts {
		if strings.EqualFold(t.nombre, nombre) {
			return true
		}
	}
	return false
}

func comoInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(x), "%d", &n)
		return n
	}
	return 0
}

func comoTexto(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return fmt.Sprint(x)
	case float64:
		return fmt.Sprint(x)
	}
	return ""
}
