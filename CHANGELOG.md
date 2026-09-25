# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/). Versionado [SemVer](https://semver.org/lang/es/).

## [Sin publicar]

### Añadido
- Licencia MIT.
- `AudioTracker.exe` autónomo: la SoundFont va dentro del ejecutable, así que basta con descargarlo y hacer doble clic. Se publica solo en *Releases* al subir una etiqueta `vX.Y.Z`.

### Cambiado
- La SoundFont GeneralUser GS está en el repositorio: `scripts\setup.ps1` ya no descarga nada y es opcional.

### Corregido
- Si la app no podía arrancar (por ejemplo, sin SoundFont), se cerraba sin decir nada. Ahora muestra el error en un cuadro de diálogo.

## [0.1.0] - 2026-09-25

Primera versión.

### Añadido
- Lectura de partituras Guitar Pro 7/8 (`.gp`): pistas, afinaciones, repeticiones, finales alternativos, cambios de compás y de tempo, ligaduras, batería.
- Tablatura (y pentagrama de batería) con cursor que sigue la reproducción; clic en un compás para saltar.
- Reproducción con SoundFont General MIDI; silencio, solo y volumen por pista; metrónomo.
- Host ASIO en Go puro, con panel del driver, selección de entrada y salida, y liberación del audio en segundo plano.
- Grabación de una entrada a WAV de 24 bits con compensación de latencia, punch-in y posición BWF para Reaper.
- Ganancia de entrada, medidores, monitor con protección contra acoples.
- Lista de tomas: escuchar sola o con la canción, favoritas, renombrar, enviar a la Papelera.
- Tema de fondo (WAV/MP3) sincronizado con la partitura: inicio del compás 1 y ajuste de velocidad, a mano o marcando a oído.
