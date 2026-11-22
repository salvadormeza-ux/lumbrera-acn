# Lumbrera + Atalaya — Iglesia ACN

Programa de proyección (reemplazo de Holyrics) y bot de transmisión para OBS, para el PC del templo.
Informática DLA SpA.

- `Lumbrera`: consola web (puerto 8092), proyector, escenario y letras para OBS.
- `Atalaya`: panel de OBS por WebSocket v5 (puerto 8080).
- Un solo ejecutable de Windows, solo librería estándar de Go.

## Compilar el instalador

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui -s -w" -o Instalar_Lumbrera_ACN.exe .
```
(El ícono y la información de versión se agregan después con `resedit`.)

Ver `recursos/LEEME.txt` y `recursos/CHANGELOG.txt`.
