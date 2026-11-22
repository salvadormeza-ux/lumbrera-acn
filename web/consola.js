// Lumbrera · consola de control
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];

// ---------- utilidades ----------
let toastT;
function toast(msg, error) {
  let t = $('.toast');
  if (!t) { t = document.createElement('div'); document.body.appendChild(t); }
  t.className = 'toast' + (error ? ' error' : '');
  t.textContent = msg;
  clearTimeout(toastT);
  toastT = setTimeout(() => t.remove(), error ? 5000 : 2200);
}

async function api(metodo, url, cuerpo) {
  const op = { method: metodo, headers: {} };
  if (cuerpo instanceof FormData) op.body = cuerpo;
  else if (cuerpo !== undefined) { op.body = JSON.stringify(cuerpo); op.headers['Content-Type'] = 'application/json'; }
  const r = await fetch(url, op);
  if (r.status === 401) { location.reload(); throw new Error('Sesión expirada'); }
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || ('Error ' + r.status));
  return j;
}
const post = (u, b) => api('POST', u, b);
const get = u => api('GET', u);

function abrirModal(id) { $(id).classList.remove('oculto'); }
function cerrarModales() { $$('.modal').forEach(m => m.classList.add('oculto')); }
$$('[data-cerrar]').forEach(b => b.onclick = cerrarModales);
$$('.modal').forEach(m => m.addEventListener('mousedown', e => { if (e.target === m) cerrarModales(); }));

const SUP = '⁰¹²³⁴⁵⁶⁷⁸⁹';
const sup = n => String(n).split('').map(d => SUP[d]).join('');

// ---------- pestañas ----------
let pestana = 'canciones';
function irPestana(t) {
  pestana = t;
  $$('#nav button').forEach(b => b.classList.toggle('sel', b.dataset.tab === t));
  $$('[data-panel]').forEach(p => p.classList.toggle('oculto', p.dataset.panel !== t));
  if (t === 'ajustes') cargarAjustes();
  if (t === 'lista') pintarLista();
}
$$('#nav button').forEach(b => b.onclick = () => irPestana(b.dataset.tab));

// ---------- conjuntos de láminas ----------
// Un conjunto: {tipo, titulo, laminas:[{texto,ref,etiqueta}], indice, ...}
let vivo = null;                  // conjunto que está en pantalla
const conj = { cancion: null, biblia: null, texto: null };

let ultimoMostrar = 0, enviando = 0;
// si otro equipo (p. ej. el celular) proyectó otra cosa, la consola deja de considerar suyo el conjunto
function sincronizarVivo(e) {
  if (!vivo || enviando || !e || Date.now() - ultimoMostrar < 700) return;
  if (e.conjunto_id === vivo.uid) {
    if (vivo.indice !== e.indice) { vivo.indice = e.indice; pintarVivos(); }
  } else if (e.conjunto_id) { vivo = null; pintarVivos(); }
}
const nuevoId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 7);

async function mostrar(c, i) {
  if (!c || !c.laminas.length) return;
  i = Math.max(0, Math.min(c.laminas.length - 1, i));
  if (!c.uid) c.uid = nuevoId();
  c.indice = i;
  vivo = c;
  ultimoMostrar = Date.now();
  pintarVivos();
  enviando++;
  try {
    if (Vivo.estado && Vivo.estado.conjunto_id === c.uid) {
      await post('/api/navegar', { indice: i });
    } else {
      await post('/api/mostrar', { tipo: c.tipo, titulo: c.titulo || '', conjunto_id: c.uid, indice: i,
        laminas: c.laminas.map(l => ({ texto: l.texto, ref: l.ref || '', etiqueta: l.etiqueta || '' })) });
    }
  } catch (e) { toast(e.message, true); }
  finally { enviando--; }
}

async function navegar(d) {
  sincronizarVivo(Vivo.estado);
  let c = vivo;
  if (!c && Vivo.estado && Vivo.estado.laminas && Vivo.estado.laminas.length) {
    // algo proyectado desde otro equipo (p. ej. el celular): avanzar en el servidor
    return post('/api/navegar', { delta: d }).catch(e => toast(e.message, true));
  }
  if (!c) {
    c = pestana === 'biblia' ? conj.biblia : pestana === 'tiempos' ? conj.texto : conj.cancion;
    if (!c) return;
    return mostrar(c, d > 0 ? 0 : c.laminas.length - 1);
  }
  const i = c.indice + d;
  if (c.tipo === 'biblia' && (i >= c.laminas.length || i < 0)) {
    // pasar al capítulo siguiente / anterior
    const nuevo = await cargarCapitulo(c.libro, c.capitulo + (d > 0 ? 1 : -1), null, true);
    if (nuevo) mostrar(nuevo, d > 0 ? 0 : nuevo.laminas.length - 1);
    return;
  }
  if (i < 0 || i >= c.laminas.length) return;
  mostrar(c, i);
}

function pintarVivos() {
  $$('.lamina[data-conj]').forEach(el => {
    const c = conj[el.dataset.conj];
    el.classList.toggle('vivo', !!c && c === vivo && Number(el.dataset.i) === c.indice);
  });
  const v = $('.lamina.vivo');
  if (v) v.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}

function pintarLaminas(cont, clave, c, numerar) {
  cont.innerHTML = '';
  c.laminas.forEach((l, i) => {
    const d = document.createElement('div');
    d.className = 'lamina';
    d.dataset.conj = clave; d.dataset.i = i;
    if (l.rango) d.classList.add('rango');
    d.innerHTML = (l.etiqueta ? `<span class="tag">${escaparHTML(l.etiqueta)}</span><br>` : '') +
      `<span class="n">${numerar ? escaparHTML(numerar(l, i)) : i + 1}</span>` + escaparHTML(l.texto);
    d.onclick = () => mostrar(c, i);
    cont.appendChild(d);
  });
  pintarVivos();
}

// ---------- modos y teclado ----------
async function modo(m) {
  try { await post('/api/modo', { modo: m }); } catch (e) { toast(e.message, true); }
}
$$('[data-modo]').forEach(b => b.onclick = () => {
  const m = b.dataset.modo;
  // pulsar otra vez el modo activo vuelve al texto
  modo(Vivo.estado && Vivo.estado.modo === m && m !== 'texto' ? 'texto' : m);
});
$('#ant').onclick = $('#antM').onclick = () => navegar(-1);
$('#sig').onclick = $('#sigM').onclick = () => navegar(1);

addEventListener('keydown', e => {
  const enCampo = /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName) && document.activeElement.type !== 'range';
  const teclasModo = { F8: 'negro', F9: 'limpio', F10: 'logo' };
  if (teclasModo[e.key]) {
    e.preventDefault();
    const m = teclasModo[e.key];
    modo(Vivo.estado && Vivo.estado.modo === m ? 'texto' : m);
    return;
  }
  if (e.key === 'Escape') {
    if (!$$('.modal').every(m => m.classList.contains('oculto'))) { cerrarModales(); return; }
    if (enCampo) { document.activeElement.blur(); return; }
    modo('texto'); return;
  }
  if (enCampo || !$$('.modal').every(m => m.classList.contains('oculto'))) return;
  if (['ArrowRight', 'ArrowDown', 'PageDown', ' '].includes(e.key)) { e.preventDefault(); navegar(1); }
  if (['ArrowLeft', 'ArrowUp', 'PageUp'].includes(e.key)) { e.preventDefault(); navegar(-1); }
});

// ---------- estado en vivo ----------
function renderEstado(e) {
  sincronizarVivo(e);
  $$('#modos [data-modo]').forEach(b => b.classList.toggle('activo', b.dataset.modo === e.modo && e.modo !== 'texto'));
  $('#modoActual').textContent = { texto: '● En pantalla', negro: '■ Negro', limpio: '○ Solo fondo', logo: '✦ Logo' }[e.modo] || e.modo;
  $('#modoActual').style.borderColor = e.modo === 'texto' ? 'var(--vivo)' : 'var(--borde)';
  const l = e.actual || {};
  $('#textoActual').textContent = l.texto || '';
  $('#refActual').textContent = [l.etiqueta, l.ref].filter(Boolean).join(' · ');
  $('#latAviso').textContent = e.aviso_activo ? '● Visible' : 'Oculto';
  $('#avisoEstado').textContent = e.aviso_activo ? '● Visible' : 'Oculto';
  $('#avisoEstado').style.borderColor = e.aviso_activo ? 'var(--ok)' : 'var(--borde)';
  if (e.crono && document.activeElement !== $('#cronoLimite')) $('#cronoLimite').value = e.crono.limite_min;
}
function conexion(ok) {
  const p = $('#conexion');
  p.querySelector('.punto').className = 'punto ' + (ok ? 'ok' : 'mal');
  p.querySelector('span:last-child').textContent = ok ? 'Conectado' : 'Sin conexión';
}
Vivo.conectar(renderEstado, conexion);

setInterval(() => {
  const e = Vivo.estado; if (!e) return;
  const r = e.cuenta.fin - Vivo.ahora();
  const c = e.cuenta.activa && r > 0 ? mmss(r + 999) : '—';
  $('#latCuenta').textContent = c; $('#cuentaReloj').textContent = c === '—' ? '--:--' : c;
  const ms = cronoMs(e.crono), lim = e.crono.limite_min * 60000;
  const t = mmss(ms);
  $('#latCrono').textContent = t; $('#cronoReloj').textContent = t;
  $('#cronoReloj').style.color = lim && ms > lim ? 'var(--vivo)' : '';
}, 250);

function ajustarPrevia() {
  const p = $('#previa'), f = p.querySelector('iframe');
  f.style.transform = `scale(${p.clientWidth / 1920})`;
}
addEventListener('resize', ajustarPrevia);

// ---------- CANCIONES ----------
let cancionSel = null;
async function buscarCanciones() {
  const q = $('#buscarCancion').value;
  const lista = await get('/api/canciones?q=' + encodeURIComponent(q));
  const cont = $('#listaCanciones');
  cont.innerHTML = lista.length ? '' : '<p class="suave pequeno">Sin resultados.</p>';
  lista.forEach(c => {
    const d = document.createElement('div');
    d.className = 'item' + (cancionSel && cancionSel.id === c.id ? ' sel' : '');
    d.innerHTML = `${escaparHTML(c.titulo)}<small>${escaparHTML(c.autor || c.inicio)}</small>`;
    d.onclick = () => abrirCancion(c.id);
    cont.appendChild(d);
  });
}
let tBuscar;
$('#buscarCancion').oninput = () => { clearTimeout(tBuscar); tBuscar = setTimeout(buscarCanciones, 150); };
$('#buscarCancion').onkeydown = e => { if (e.key === 'Enter') { const p = $('#listaCanciones .item'); p && p.click(); } };

async function abrirCancion(id, proyectar) {
  const c = await get('/api/canciones/' + encodeURIComponent(id));
  cancionSel = c;
  conj.cancion = { tipo: 'cancion', id: c.id, titulo: c.titulo, laminas: c.laminas, indice: -1 };
  $('#cancionVacia').classList.add('oculto');
  $('#cancionDetalle').classList.remove('oculto');
  $('#cTitulo').textContent = c.titulo;
  $('#cAutor').textContent = [c.autor, c.tono && 'Tono: ' + c.tono].filter(Boolean).join(' · ');
  pintarLaminas($('#laminasCancion'), 'cancion', conj.cancion);
  $$('#listaCanciones .item').forEach(x => x.classList.remove('sel'));
  buscarCanciones();
  if (proyectar) mostrar(conj.cancion, 0);
}

function editorCancion(c) {
  c = c || {};
  $('#mcTitulo').textContent = c.id ? 'Editar canción' : 'Nueva canción';
  $('#mcNombre').value = c.titulo || '';
  $('#mcAutor').value = c.autor || '';
  $('#mcTono').value = c.tono || '';
  $('#mcLetra').value = c.letra || '';
  $('#mcGuardar').onclick = async () => {
    try {
      const r = await post('/api/canciones', { id: c.id || '', titulo: $('#mcNombre').value, autor: $('#mcAutor').value, tono: $('#mcTono').value, letra: $('#mcLetra').value });
      cerrarModales(); toast('Canción guardada');
      abrirCancion(r.id);
    } catch (e) { toast(e.message, true); }
  };
  abrirModal('#modalCancion');
  setTimeout(() => $('#mcNombre').focus(), 50);
}
$('#nuevaCancion').onclick = () => editorCancion();
$('#cEditar').onclick = () => cancionSel && editorCancion(cancionSel);
$('#cBorrar').onclick = async () => {
  if (!cancionSel || !confirm(`¿Borrar la canción «${cancionSel.titulo}»?`)) return;
  try {
    await api('DELETE', '/api/canciones/' + encodeURIComponent(cancionSel.id));
    cancionSel = null; conj.cancion = null;
    $('#cancionDetalle').classList.add('oculto'); $('#cancionVacia').classList.remove('oculto');
    buscarCanciones(); toast('Canción borrada');
  } catch (e) { toast(e.message, true); }
};
$('#cALista').onclick = () => cancionSel && agregarALista({ tipo: 'cancion', id: cancionSel.id, titulo: cancionSel.titulo });
$('#importarTxt').onclick = () => $('#archivosTxt').click();
$('#archivosTxt').onchange = async e => {
  const fd = new FormData();
  [...e.target.files].forEach(f => fd.append('archivos', f));
  e.target.value = '';
  try {
    const r = await post('/api/canciones/importar', fd);
    toast(`${r.importadas} canción(es) importada(s) o actualizada(s)` + (r.errores && r.errores.length ? ' · ' + r.errores.length + ' con error' : ''), r.errores && r.errores.length);
    buscarCanciones();
  } catch (err) { toast(err.message, true); }
};

// ---------- BIBLIA ----------
let libros = [], bibliaInfo = { libro: 0 };
function sigla() { return $('#selBiblia').value; }

async function cargarBiblias(pref) {
  const bs = await get('/api/biblias');
  const s = $('#selBiblia'), actual = pref || s.value;
  s.innerHTML = bs.map(b => `<option value="${escaparHTML(b.sigla)}" title="${escaparHTML(b.nombre)}">${escaparHTML(b.sigla)}</option>`).join('');
  if (bs.some(b => b.sigla === actual)) s.value = actual;
  $('#listaBiblias').innerHTML = bs.map(b => `<div class="item fila"><div class="crece">${escaparHTML(b.sigla)}<small>${escaparHTML(b.nombre)} · ${b.versiculos.toLocaleString('es-CL')} versículos</small></div>
    <button class="btn chico" data-pred="${escaparHTML(b.sigla)}">Predeterminada</button><button class="btn chico peligro" data-borrar="${escaparHTML(b.sigla)}">Quitar</button></div>`).join('');
  $$('[data-pred]').forEach(b => b.onclick = async () => { await post('/api/ajustes', { biblia_predeterminada: b.dataset.pred }); toast(b.dataset.pred + ' quedó como predeterminada'); });
  $$('[data-borrar]').forEach(b => b.onclick = async () => {
    if (!confirm('¿Quitar la Biblia ' + b.dataset.borrar + ' de Lumbrera?')) return;
    try { await api('DELETE', '/api/biblias/' + encodeURIComponent(b.dataset.borrar)); cargarBiblias(); } catch (e) { toast(e.message, true); }
  });
  await cargarLibros();
}

async function cargarLibros() {
  if (!sigla()) return;
  libros = await get('/api/libros?biblia=' + encodeURIComponent(sigla()));
  const boton = l => `<button data-libro="${l.num}" class="${l.testamento === 'NT' ? 'nt' : ''}" title="${escaparHTML(l.nombre)}" ${l.capitulos ? '' : 'disabled style="opacity:.35"'}>${escaparHTML(l.corto)}</button>`;
  $('#librosAT').innerHTML = libros.filter(l => l.testamento === 'AT').map(boton).join('');
  $('#librosNT').innerHTML = libros.filter(l => l.testamento === 'NT').map(boton).join('');
  $$('[data-libro]').forEach(b => b.onclick = () => elegirLibro(Number(b.dataset.libro)));
  if (bibliaInfo.libro) elegirLibro(bibliaInfo.libro, true);
}

function elegirLibro(n, silencioso) {
  bibliaInfo.libro = n;
  $$('[data-libro]').forEach(b => b.classList.toggle('sel', Number(b.dataset.libro) === n));
  const l = libros[n - 1];
  $('#caps').innerHTML = Array.from({ length: l.capitulos }, (_, i) => `<button data-cap="${i + 1}">${i + 1}</button>`).join('');
  $$('[data-cap]').forEach(b => b.onclick = () => cargarCapitulo(n, Number(b.dataset.cap)));
  if (l.capitulos === 1 && !silencioso) cargarCapitulo(n, 1);
}

function armarLaminasBiblia(r, por) {
  const nombre = r.nombre, lam = [];
  for (let i = 0; i < r.versiculos.length; i += por) {
    const grupo = [];
    for (let k = i; k < Math.min(i + por, r.versiculos.length); k++) if (r.versiculos[k]) grupo.push(k + 1);
    if (!grupo.length) continue;
    const v1 = grupo[0], v2 = grupo[grupo.length - 1];
    const texto = grupo.length === 1 ? r.versiculos[v1 - 1] : grupo.map(v => sup(v) + ' ' + r.versiculos[v - 1]).join(' ');
    lam.push({ texto, ref: `${nombre} ${r.capitulo}:${v1}${v2 > v1 ? '-' + v2 : ''} (${r.sigla})`, v1, v2,
      rango: r.desde && v2 >= r.desde && v1 <= (r.hasta || r.desde) });
  }
  return lam;
}

// cargarCapitulo devuelve el conjunto; con refTexto usa la referencia escrita.
async function cargarCapitulo(libro, cap, refTexto, silencioso) {
  let r;
  try {
    r = await get('/api/capitulo?biblia=' + encodeURIComponent(sigla()) +
      (refTexto ? '&ref=' + encodeURIComponent(refTexto) : `&libro=${libro}&capitulo=${cap}`));
  } catch (e) { if (!silencioso || refTexto) toast(e.message, true); return null; }
  const por = Number($('#porLamina').value) || 1;
  const c = { tipo: 'biblia', titulo: `${r.nombre} ${r.capitulo}`, libro: r.libro, capitulo: r.capitulo,
    sigla: r.sigla, laminas: armarLaminasBiblia(r, por), indice: -1, datos: r };
  const anterior = conj.biblia;
  conj.biblia = c;
  if (vivo === anterior) vivo = null;
  bibliaInfo.libro = r.libro;
  $$('[data-libro]').forEach(b => b.classList.toggle('sel', Number(b.dataset.libro) === r.libro));
  if (libros.length) elegirLibro(r.libro, true);
  $$('[data-cap]').forEach(b => b.classList.toggle('sel', Number(b.dataset.cap) === r.capitulo));
  $('#tituloCapitulo').textContent = `${r.nombre} ${r.capitulo}  ·  ${r.sigla}`;
  pintarLaminas($('#versos'), 'biblia', c, l => l.v2 > l.v1 ? `${l.v1}-${l.v2}` : String(l.v1));
  return c;
}

async function irReferencia() {
  const t = $('#refBiblia').value.trim();
  if (!t) return;
  const c = await cargarCapitulo(0, 0, t);
  if (!c) return;
  const desde = c.datos.desde;
  let i = 0;
  if (desde) i = Math.max(0, c.laminas.findIndex(l => l.v1 <= desde && l.v2 >= desde));
  if (desde) mostrar(c, i);
  else $('#versos').scrollIntoView({ behavior: 'smooth' });
}
$('#irRef').onclick = irReferencia;
$('#refBiblia').onkeydown = e => { if (e.key === 'Enter') { e.preventDefault(); e.target.blur(); irReferencia(); } };
$('#selBiblia').onchange = async () => {
  await cargarLibros();
  const c = conj.biblia;
  if (c) {
    const idx = c.indice, eraVivo = vivo === c;
    const n = await cargarCapitulo(c.libro, c.capitulo);
    if (n && eraVivo && idx >= 0) mostrar(n, idx);
  }
};
$('#porLamina').onchange = () => { const c = conj.biblia; if (c) cargarCapitulo(c.libro, c.capitulo); };
$('#capAnt').onclick = () => conj.biblia && cargarCapitulo(conj.biblia.libro, conj.biblia.capitulo - 1);
$('#capSig').onclick = () => conj.biblia && cargarCapitulo(conj.biblia.libro, conj.biblia.capitulo + 1);
$('#bALista').onclick = () => {
  const c = conj.biblia; if (!c) return toast('Primero abra un capítulo', true);
  const l = c.indice >= 0 ? c.laminas[c.indice] : null;
  const ref = l ? `${c.datos.nombre} ${c.capitulo}:${l.v1}` : `${c.datos.nombre} ${c.capitulo}`;
  agregarALista({ tipo: 'biblia', ref, biblia: c.sigla, titulo: ref + ' (' + c.sigla + ')' });
};

let tTexto;
$('#buscarTexto').oninput = () => {
  clearTimeout(tTexto);
  tTexto = setTimeout(async () => {
    const q = $('#buscarTexto').value.trim();
    const cont = $('#resultados');
    if (q.length < 3) { cont.innerHTML = ''; return; }
    const res = await get(`/api/buscar-biblia?biblia=${encodeURIComponent(sigla())}&q=${encodeURIComponent(q)}`);
    cont.innerHTML = res.length ? '' : '<p class="suave pequeno">Sin resultados.</p>';
    res.slice(0, 60).forEach(r => {
      const d = document.createElement('div');
      d.className = 'item';
      d.innerHTML = `<b>${escaparHTML(r.ref)}</b><small>${escaparHTML(r.texto)}</small>`;
      d.onclick = async () => {
        const c = await cargarCapitulo(r.libro, r.capitulo);
        if (!c) return;
        const i = c.laminas.findIndex(l => l.v1 <= r.versiculo && l.v2 >= r.versiculo);
        mostrar(c, Math.max(0, i));
      };
      cont.appendChild(d);
    });
    if (res.length >= 100) cont.insertAdjacentHTML('beforeend', '<p class="suave pequeno">Se muestran los primeros resultados; agregue más palabras para afinar.</p>');
  }, 300);
};

$('#importarBiblia').onclick = () => { $('#mbEstado').textContent = ''; abrirModal('#modalBiblia'); };
$('#mbImportar').onclick = async () => {
  const fd = new FormData();
  const f = $('#mbArchivo').files[0];
  if (f) fd.append('archivo', f);
  else if ($('#mbUrl').value.trim()) fd.append('url', $('#mbUrl').value.trim());
  else { $('#mbEstado').textContent = 'Seleccione un archivo o pegue un enlace.'; return; }
  fd.append('sigla', $('#mbSigla').value.trim());
  fd.append('nombre', $('#mbNombre').value.trim());
  $('#mbEstado').textContent = '⏳ Importando… (puede tardar unos segundos)';
  $('#mbImportar').disabled = true;
  try {
    const r = await post('/api/biblias/importar', fd);
    $('#mbEstado').textContent = `✅ ${r.nombre} (${r.sigla}) · ${r.versiculos.toLocaleString('es-CL')} versículos · formato ${r.formato}`;
    $('#mbArchivo').value = ''; $('#mbUrl').value = '';
    await cargarBiblias(r.sigla);
  } catch (e) { $('#mbEstado').textContent = '❌ ' + e.message; }
  $('#mbImportar').disabled = false;
};

// ---------- LISTA DEL CULTO ----------
let lista = [];
async function cargarLista() { lista = await get('/api/lista'); pintarLista(); }
async function guardarLista() { try { await post('/api/lista', lista); } catch (e) { toast(e.message, true); } pintarLista(); }
function agregarALista(item) { lista.push(item); guardarLista(); toast('Agregado a la lista del culto'); }
function pintarLista() {
  const cont = $('#listaCulto');
  cont.innerHTML = lista.length ? '' : '<p class="suave">La lista está vacía.</p>';
  const ico = { cancion: '🎵', biblia: '📖', texto: '📝' };
  lista.forEach((it, i) => {
    const d = document.createElement('div');
    d.className = 'item fila';
    d.innerHTML = `<span class="suave" style="width:22px">${i + 1}</span><div class="crece">${ico[it.tipo] || ''} ${escaparHTML(it.titulo)}</div>
      <button class="btn chico" data-a="sub" title="Subir">↑</button><button class="btn chico" data-a="baj" title="Bajar">↓</button><button class="btn chico peligro" data-a="del" title="Quitar">✕</button>`;
    d.onclick = ev => {
      const a = ev.target.dataset.a;
      if (a === 'sub' && i > 0) { [lista[i - 1], lista[i]] = [lista[i], lista[i - 1]]; guardarLista(); }
      else if (a === 'baj' && i < lista.length - 1) { [lista[i + 1], lista[i]] = [lista[i], lista[i + 1]]; guardarLista(); }
      else if (a === 'del') { lista.splice(i, 1); guardarLista(); }
      else if (!a) abrirItem(it);
    };
    cont.appendChild(d);
  });
}
async function abrirItem(it) {
  if (it.tipo === 'cancion') { irPestana('canciones'); abrirCancion(it.id, true); }
  else if (it.tipo === 'biblia') {
    irPestana('biblia');
    if (it.biblia && [...$('#selBiblia').options].some(o => o.value === it.biblia)) { $('#selBiblia').value = it.biblia; await cargarLibros(); }
    $('#refBiblia').value = it.ref; irReferencia();
  } else if (it.tipo === 'texto') proyectarTexto(it.titulo, it.texto);
}
$('#listaVaciar').onclick = () => { if (lista.length && confirm('¿Vaciar la lista del culto?')) { lista = []; guardarLista(); } };
$('#listaTexto').onclick = () => { irPestana('tiempos'); $('#libreTexto').focus(); };

// ---------- AVISOS Y TIEMPOS ----------
$('#avisoMostrar').onclick = () => post('/api/aviso', { texto: $('#avisoTexto').value, activo: true }).catch(e => toast(e.message, true));
$('#avisoOcultar').onclick = () => post('/api/aviso', { texto: $('#avisoTexto').value, activo: false });
$('#cuentaIniciar').onclick = () => post('/api/cuenta', { minutos: Number($('#cuentaMin').value), texto: $('#cuentaTexto').value, activa: true });
$('#cuentaDetener').onclick = () => post('/api/cuenta', { minutos: 0, texto: $('#cuentaTexto').value, activa: false });
$$('[data-crono]').forEach(b => b.onclick = () => post('/api/crono', { accion: b.dataset.crono, limite_min: Number($('#cronoLimite').value) }));
$('#cronoLimite').onchange = () => post('/api/crono', { accion: '', limite_min: Number($('#cronoLimite').value) });
$('#msgEnviar').onclick = () => post('/api/escenario', { mensaje: $('#msgEscenario').value }).then(() => toast('Mensaje enviado al escenario'));
$('#msgEscenario').onkeydown = e => { if (e.key === 'Enter') $('#msgEnviar').click(); };
$('#msgBorrar').onclick = () => { $('#msgEscenario').value = ''; post('/api/escenario', { mensaje: '' }); };

function proyectarTexto(titulo, texto) {
  const bloques = (texto || '').split(/\n\s*\n/).map(b => b.trim()).filter(Boolean);
  if (!bloques.length) return toast('Escriba un texto', true);
  conj.texto = { tipo: 'texto', titulo, laminas: bloques.map(b => ({ texto: b, ref: titulo || '' })), indice: -1 };
  mostrar(conj.texto, 0);
}
$('#libreProyectar').onclick = () => proyectarTexto($('#libreTitulo').value.trim(), $('#libreTexto').value);
$('#libreALista').onclick = () => {
  const t = $('#libreTexto').value.trim(); if (!t) return toast('Escriba un texto', true);
  agregarALista({ tipo: 'texto', titulo: $('#libreTitulo').value.trim() || t.split('\n')[0].slice(0, 40), texto: t });
};

// ---------- AJUSTES ----------
let info = null;
function opciones(sel, lista, valor, vacio) {
  sel.innerHTML = `<option value="">${vacio}</option>` + lista.map(f => `<option>${escaparHTML(f)}</option>`).join('');
  sel.value = valor || '';
}
async function cargarAjustes() {
  info = await get('/api/info');
  const s = info.estilo;
  $('#aIglesia').value = info.iglesia;
  $('#aFuente').value = s.fuente; $('#aAlineacion').value = s.alineacion || 'center';
  $('#aTamano').value = s.tamano_max; $('#aTamanoV').textContent = s.tamano_max;
  $('#aColorTexto').value = s.color_texto; $('#aColorFondo').value = s.color_fondo; $('#aColorAviso').value = s.color_aviso || '#9bd3ff';
  opciones($('#aImagen'), info.fondos, s.imagen_fondo, '— Sin imagen (solo color) —');
  opciones($('#aLogo'), info.fondos, s.logo, '— Sin logo (muestra el nombre) —');
  $('#aOsc').value = s.oscurecer; $('#aOscV').textContent = s.oscurecer;
  $('#aSombra').checked = s.sombra; $('#aMayus').checked = s.mayusculas; $('#aRef').checked = s.mostrar_referencia;
  $('#aObs').value = s.obs_tamano; $('#aObsV').textContent = s.obs_tamano; $('#aFranja').checked = s.obs_franja;
  $('#aAbrirProy').checked = info.abrir_proyector;
  $('#version').textContent = 'v' + info.version;
  $('#avisoClave').classList.toggle('oculto', !info.clave_inicial);
  const ips = info.ips.length ? info.ips : ['IP-DEL-PC'];
  $('#direcciones').innerHTML = 'Conecte el celular al mismo Wi-Fi y abra:<br>' +
    ips.map(ip => `<code>http://${ip}:${info.puerto}</code>`).join('<br>') +
    `<br><br>📱 <b>Control remoto</b> (botones grandes): <code>http://${ips[0]}:${info.puerto}/control</code><br>Escenario: <code>/escenario</code> · OBS: <code>/obs</code> · Proyector: <code>/proyector</code>`;
  try {
    const ps = await get('/api/pantallas');
    $('#aPantalla').innerHTML = '<option value="0">Automática (la que no es principal)</option>' +
      ps.map(p => `<option value="${p.num}">Pantalla ${p.num} — ${p.ancho}×${p.alto}${p.principal ? ' (principal)' : ''}</option>`).join('');
    $('#aPantalla').value = info.pantalla_proyector || 0;
  } catch (e) { }
  cargarBiblias();
}
['aTamano', 'aOsc', 'aObs'].forEach(id => $('#' + id).oninput = () => { $('#' + id + 'V').textContent = $('#' + id).value; });
function estiloForm() {
  return {
    fuente: $('#aFuente').value, alineacion: $('#aAlineacion').value, tamano_max: Number($('#aTamano').value),
    color_texto: $('#aColorTexto').value, color_fondo: $('#aColorFondo').value, color_aviso: $('#aColorAviso').value,
    imagen_fondo: $('#aImagen').value, oscurecer: Number($('#aOsc').value), logo: $('#aLogo').value,
    sombra: $('#aSombra').checked, mayusculas: $('#aMayus').checked, mostrar_referencia: $('#aRef').checked,
    obs_tamano: Number($('#aObs').value), obs_franja: $('#aFranja').checked,
  };
}
$('#guardarEstilo').onclick = async () => {
  try {
    await post('/api/ajustes', { iglesia: $('#aIglesia').value, estilo: estiloForm() });
    $('#iglesia').textContent = $('#aIglesia').value;
    toast('Apariencia guardada');
  } catch (e) { toast(e.message, true); }
};
let destinoSubida = null;
$$('[data-subir]').forEach(b => b.onclick = () => { destinoSubida = b.dataset.subir; $('#archivoImagen').click(); });
$('#archivoImagen').onchange = async e => {
  const f = e.target.files[0]; e.target.value = '';
  if (!f) return;
  const fd = new FormData(); fd.append('archivo', f);
  try {
    const r = await post('/api/subir-imagen', fd);
    const actualImg = $('#aImagen').value, actualLogo = $('#aLogo').value;
    const i2 = await get('/api/info');
    opciones($('#aImagen'), i2.fondos, destinoSubida === 'aImagen' ? r.nombre : actualImg, '— Sin imagen (solo color) —');
    opciones($('#aLogo'), i2.fondos, destinoSubida === 'aLogo' ? r.nombre : actualLogo, '— Sin logo (muestra el nombre) —');
    toast('Imagen subida · recuerde «Guardar apariencia»');
  } catch (err) { toast(err.message, true); }
};
$('#guardarProy').onclick = async () => {
  try { await post('/api/ajustes', { pantalla_proyector: Number($('#aPantalla').value), abrir_proyector: $('#aAbrirProy').checked }); toast('Guardado'); }
  catch (e) { toast(e.message, true); }
};
$('#abrirProy').onclick = async () => { try { await post('/api/abrir-proyector'); toast('Abriendo proyector…'); } catch (e) { toast(e.message, true); } };
$('#guardarClave').onclick = async () => {
  try {
    await post('/api/ajustes', { clave_actual: $('#claveActual').value, clave_nueva: $('#claveNueva').value });
    $('#claveActual').value = $('#claveNueva').value = '';
    $('#avisoClave').classList.add('oculto');
    toast('Clave cambiada');
  } catch (e) { toast(e.message, true); }
};
$('#salir').onclick = async () => { await post('/api/logout'); location.reload(); };

// ---------- inicio ----------
(async () => {
  ajustarPrevia();
  try {
    info = await get('/api/info');
    $('#iglesia').textContent = info.iglesia;
    if (info.clave_inicial) setTimeout(() => toast('Recuerde cambiar la clave inicial en Ajustes'), 1500);
    await buscarCanciones();
    await cargarBiblias(info.biblia_predeterminada);
    await cargarLista();
  } catch (e) { toast(e.message, true); }
})();
