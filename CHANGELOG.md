# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/). Versionado [SemVer](https://semver.org/lang/es/).

## [Sin publicar]

### Añadido
- Licencia MIT.

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
