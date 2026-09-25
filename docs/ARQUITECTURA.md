# Arquitectura

## Paquetes

| Paquete | Responsabilidad | Archivos clave |
|---|---|---|
| `main.go` | Crea el motor con la SoundFont incluida en el ejecutable y lanza la UI; los errores de arranque salen en un cuadro de diálogo | `soundfont.go` |
| `internal/asio` | Host ASIO en Go puro: carga el driver COM y llama a la vtable `IASIO` con `syscall` | `asio.go` |
| `internal/gp` | Parser de GP7/8, despliegue de repeticiones, mapa de tempo y eventos MIDI | `parse.go`, `timeline.go`, `model.go` |
| `internal/engine` | Audio en tiempo real: sintetizador, secuenciador, mezcla, grabación y E/S de WAV/MP3 | `engine.go`, `recorder.go`, `wav*.go`, `audio.go` |
| `internal/ui` | Interfaz con Gio: ventana, tablatura, pistas, tomas y tema | `app.go`, `tab.go`, `takes.go`, `backing.go` |
| `internal/testutil` | Fixtures de test (partitura mínima, SoundFont) | |
| `cmd/asiotest` | Diagnóstico con el driver real: reproduce y graba 3 s en silencio | |

Dependencias: `ui → engine → gp`, `engine → asio`. `gp` y `asio` no dependen de nada del proyecto.

## Flujo de datos

```
.gp (zip) ──gp.Load──▶ Song ──gp.NewTimeline──▶ Timeline (repeticiones desplegadas, tempo)
                                     │
                               gp.Events ──▶ eventos en segundos de partitura
                                     │
               engine.rebuildEvents (+ sincronización) ──▶ eventos en muestras de sesión
                                     │
driver ASIO ──callback──▶ engine.process: sintetizador + tema + monitor + toma ──▶ salida
                                     └──▶ ring buffer ──▶ Recorder (goroutine) ──▶ WAV
```

## Hilos: reglas que no se pueden romper

- **Hilo de audio** (`engine.process`, llamado por el driver): no puede bloquearse, esperar a un mutex, hacer E/S ni reservar memoria en cada bloque. Todo su estado vive en `engine.rt`.
- **Goroutine de UI** (el bucle de eventos de Gio): llama a los métodos exportados de `Engine`. Nunca toca `rt` directamente mientras el audio corre: usa `e.exec(func(*rt){…})`, que encola el cambio y espera a que el hilo de audio lo aplique (con timeout).
- **Del hilo de audio hacia fuera** solo se comunica con atómicos (`pos`, `playing`, niveles de pico…) y con el ring buffer SPSC de la grabación.
- **Driver ASIO**: sus llamadas de control van al hilo COM (STA) propio de `asio.Driver` (`comThread`). Solo puede haber un driver abierto por proceso.
- **Trabajo lento** (diálogos de archivo, decodificar MP3, panel ASIO): va en una goroutine aparte, que devuelve el resultado a la UI con `a.post(func(){…})`.

## Tiempos y unidades

- **Ticks**: 960 por negra (`gp.TicksPerQuarter`). Las posiciones dentro de un compás van en ticks.
- **Tiempo de partitura**: segundos desde el compás 1, según el mapa de tempo (`Timeline.TickToSec`).
- **Tiempo de sesión**: el reloj de reproducción. Sin tema cargado, es igual al tiempo de partitura. Con tema, es el tiempo del audio: `sesión = offset + partitura × scale` (`Engine.SetSync`, `ui.gpTime` / `ui.sessionTime`).
- **Muestras**: tiempo de sesión × frecuencia del driver. `Engine.Position()` y `Seek()` trabajan en segundos de sesión.
- **Tomas**: se escriben en tiempo de sesión. La posición va en el campo `TimeReference` del chunk `bext` (BWF). La grabación descarta la latencia de entrada + salida (+ `LatencyAdjust`) para quedar alineada.

## Estado persistente

| Dónde | Qué | Código |
|---|---|---|
| `%AppData%\audio_tracker\config.json` | Ajustes del usuario | `ui/config.go` |
| `<gp> - tomas\*.wav` | Tomas BWF de 24 bits | `engine/wav.go` |
| `<gp> - tomas\tomas.json` | Favoritas | `ui/takes.go` |
| `<gp> - tomas\proyecto.json` | Tema y sincronización | `ui/backing.go` |

Cambiar un formato persistente exige seguir leyendo los archivos antiguos (añade campos, no los renombres).

## Convenciones

- **Idioma**: los textos de la interfaz y la documentación, en español. El código y los comentarios, en inglés.
- **Sin cgo**: solo Go puro y llamadas al sistema con `golang.org/x/sys/windows`.
- **Solo Windows**: ASIO, los diálogos de archivo y la Papelera son de Windows. Si se añade otra plataforma, aíslala con build tags (`_windows.go`).
- **Sin avisos modales**: los errores y confirmaciones van a la barra de estado (`a.setStatus`) o se muestran en la propia fila.
