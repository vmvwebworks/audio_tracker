# Manual de uso

## Instalar

Descarga `AudioTracker.exe` de la [última versión](https://github.com/vmvwebworks/audio_tracker/releases/latest) y haz doble clic: no se instala nada más. Guárdalo donde quieras (por ejemplo en el Escritorio). Si Windows avisa de que es una aplicación desconocida, pulsa *Más información* → *Ejecutar de todas formas*.

Necesitas un driver ASIO para tu tarjeta de sonido; si no tiene uno propio, usa [ASIO4ALL](https://asio4all.org).

## Primeros pasos

1. **Abrir** (icono de carpeta): carga una partitura `.gp` de Guitar Pro 7/8. La app recuerda la última que abriste.
2. Elige en la barra de dispositivo el **driver ASIO**, la **entrada** donde está conectado tu instrumento y la **salida**.
3. **Reproducir / Pausa** (Espacio). **Parar** (Inicio): vuelve al punto donde empezaste; si lo pulsas otra vez, vuelve al principio.
4. **Clic en un compás** de la tablatura: la reproducción empieza desde ahí.

## Pistas

- Haz clic en el nombre de una pista para ver su tablatura. La batería se dibuja en pentagrama.
- **M** silencia la pista, **S** la deja sola y el deslizador ajusta su volumen.
- **Metrónomo** y **Seguir cursor** están en la barra superior.

## Grabar

La tarjeta **Grabación** es la pista de tu instrumento.

- **REC** (tecla R) arma la pista. Con la pista armada, Reproducir graba.
- Si la armas con la canción sonando, empieza a grabar en ese momento (punch-in). Si la desarmas, la toma termina y la canción sigue.
- El botón de grabar de la barra superior arma y arranca en un solo paso.
- **Ganancia** (0 a +40 dB): súbela hasta que el nivel llegue a unos −12 dB al tocar fuerte.
- **Monitor** (auriculares): te escuchas por la app mientras la pista está armada. Viene apagado. Úsalo con auriculares: si la app detecta un acople, lo apaga sola.

Las tomas se guardan en `<carpeta del .gp>/<nombre> - tomas/` como WAV mono de 24 bits. El nombre indica dónde empiezan, por ejemplo `toma 2026-09-25 10.12.03 - compás 18 (0-35).wav`. Cada toma lleva su posición en la canción (marca BWF): en Reaper, tras insertarla, usa *Item: Move to source preferred position (used by BWF)* y queda en su sitio.

## Tomas

Debajo de las pistas aparece la lista de tomas de la canción, de la más nueva a la más antigua.

- **▶** escucha la toma sola. **Con la canción** la reproduce en su sitio desde su compás.
- **⭐** marca la toma como favorita. La estrella de la cabecera filtra la lista para ver solo las favoritas.
- **✏️** renombra el WAV (Enter guarda, Esc cancela). **🗑️** lo envía a la Papelera de Windows, tras confirmar.
- El icono de carpeta de la tarjeta Grabación abre la carpeta de tomas en el Explorador.

## Tocar sobre el tema real

1. Exporta el tema en Reaper (*File → Render*) a WAV o MP3.
2. En la tarjeta **Tema (audio)**, pulsa la carpeta y cárgalo. Tiene volumen y botón para silenciarlo.
3. **Compás 1**: indica en qué momento del audio empieza la partitura, para saltar la intro. Tienes dos formas:
   - haz clic en el valor y escribe el tiempo que ves en Reaper (`0:07.350` o `7,35`);
   - o, con el tema sonando, pulsa **I** (o la banderita) justo cuando empiece el compás 1.

   Afina de 10 en 10 ms con − / +.
4. **Velocidad** (solo si la partitura se va desviando): escribe el porcentaje, o pulsa **T** en el primer tiempo de un compás lejano del inicio.

La sincronización se guarda por canción. Con el tema cargado, la línea de tiempo es la del audio: la barra superior muestra "Intro 0:07" hasta el compás 1, y las tomas se colocan en el tiempo del audio. El ajuste de velocidad es lineal: si el tema acelera o frena, la partitura se irá desviando.

## Audio y ASIO

- **ASIO4ALL**: pulsa **Panel ASIO** y activa tu tarjeta (y su entrada) en la lista de dispositivos. La app se reinicia sola con la nueva configuración. Un búfer de 128 o 256 muestras da buena latencia.
- Si la entrada aparece como «Not Connected», ASIO4ALL no tiene ninguna entrada activa o la está usando otra aplicación.
- **Ajuste latencia**: si las tomas quedan un poco adelantadas o retrasadas, corrígelo en pasos de 1 ms.
- ASIO4ALL usa la tarjeta en exclusiva. Cuando la ventana pasa a segundo plano sin reproducir, grabar ni tener REC armado, la app **libera el audio** para otros programas y lo recupera al volver.

## Atajos

| Tecla | Acción |
|---|---|
| Espacio | Reproducir / pausa |
| Inicio | Parar |
| R | Armar grabación |
| I | Marcar el compás 1 en el tema |
| T | Marcar la velocidad del tema |

## Archivos

| Archivo | Contenido |
|---|---|
| `%AppData%\audio_tracker\config.json` | Driver, canales, ganancia, monitor, última partitura… |
| `<nombre> - tomas\*.wav` | Tomas (Broadcast WAV) |
| `<nombre> - tomas\tomas.json` | Favoritas |
| `<nombre> - tomas\proyecto.json` | Tema cargado y su sincronización |

## Limitaciones

- **Formatos**: solo GP7/8 (`.gp`); todavía no `.gp5` ni `.gpx`.
- **Navegación**: no sigue D.S., D.C. ni Coda (las repeticiones y los finales alternativos, sí).
- **Efectos**: en la reproducción solo se interpretan la nota muerta, el palm mute, la nota fantasma y el acento.
- **Sonido**: sale de una SoundFont General MIDI, no de los sonidos RSE de Guitar Pro.
