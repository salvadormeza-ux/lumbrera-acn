// Conexión en vivo con Lumbrera (Server-Sent Events) y utilidades compartidas.
const Vivo = {
  estado: null, desfase: 0, conectado: false,
  conectar(cb, cbConexion) {
    let es;
    const abrir = () => {
      es = new EventSource('/api/eventos');
      es.onmessage = ev => {
        const e = JSON.parse(ev.data);
        this.desfase = e.ahora - Date.now();
        this.estado = e;
        if (!this.conectado) { this.conectado = true; cbConexion && cbConexion(true); }
        cb(e);
      };
      es.onerror = () => {
        if (this.conectado) { this.conectado = false; cbConexion && cbConexion(false); }
        es.close(); setTimeout(abrir, 1500);
      };
    };
    abrir();
  },
  ahora() { return Date.now() + this.desfase; }
};

function mmss(ms) {
  const neg = ms < 0; ms = Math.abs(ms);
  const s = Math.floor(ms / 1000), h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, x = s % 60;
  const p = n => String(n).padStart(2, '0');
  return (neg ? '-' : '') + (h ? h + ':' + p(m) : p(m)) + ':' + p(x);
}

function cronoMs(c) {
  if (!c) return 0;
  return c.acumulado + (c.corriendo ? Vivo.ahora() - c.inicio : 0);
}

// Ajusta el tamaño de letra para que el texto quepa en su caja.
function ajustarTexto(el, caja, maxPx, minPx) {
  minPx = minPx || 18;
  let lo = minPx, hi = Math.max(minPx, maxPx), mejor = minPx;
  const W = caja.clientWidth, H = caja.clientHeight;
  for (let i = 0; i < 12 && lo <= hi; i++) {
    const mid = Math.floor((lo + hi) / 2);
    el.style.fontSize = mid + 'px';
    if (el.scrollWidth <= W + 1 && el.scrollHeight <= H + 1) { mejor = mid; lo = mid + 1; } else hi = mid - 1;
  }
  el.style.fontSize = mejor + 'px';
}

function escaparHTML(s) {
  return String(s || '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
