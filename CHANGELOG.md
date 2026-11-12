# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/). Versionado [SemVer](https://semver.org/lang/es/).

## [Sin publicar]

### Añadido
- **Afinador**: botón a la derecha de *Seguir cursor*. Detecta la nota que tocas por la entrada de audio (algoritmo YIN, de unos 28 Hz a 1,4 kHz), muestra la nota, la frecuencia y una aguja en cents, y marca las cuerdas de la afinación de la pista seleccionada, que se pueden fijar como objetivo. Se elige el instrumento (pista, guitarra, Drop D, 7 cuerdas, bajo de 4 y 5, ukelele, mandolina/violín, banjo o cromático) y se puede cambiar la nota de cada cuerda (medio tono u octava). La referencia (La = 440 Hz por defecto) se ajusta de 430 a 450 Hz y se guarda en `config.json` (`tuner_a4`, `tuner_instrument`, `tuner_custom`).
- **Cuenta atrás antes de grabar**: con la pista armada, Reproducir da 1 o 2 compases de clics (en el compás y el tempo de donde empiezas) antes de que arranquen la canción y la toma. Se elige con el botón *Cuenta* de la barra (no, 1 o 2; por defecto 1) y se guarda en `config.json` (`count_in_bars`).
- **Zoom de la partitura** (− / + en la barra, del 50 % al 250 %), guardado en `config.json` (`zoom`).
- **Partitura en pentagrama** encima de la tablatura, alineada con ella: clave de Sol, Fa o percusión, armadura (leída del `.gp`), compás, figuras con plicas, corchetes y barras, silencios, puntillos, tresillos, ligaduras, notas de adorno y alteraciones.

### Cambiado
- Los números de la tablatura son más grandes, negros y en negrita, con más separación entre cuerdas.
- Solo se dibuja la primera voz de cada pista; las demás siguen sonando.
- La batería también se dibuja ahora con clave de percusión, plicas y barras.
- Las filas de la partitura ajustan su altura a las notas muy agudas o graves. Se quitan las plicas bajo la tablatura (el ritmo está en el pentagrama) y las letras de la afinación a la izquierda (sustituidas por «TAB»).
- Con el afinador abierto, la app no libera el audio al pasar a segundo plano.

### Corregido
- El `.exe` no se adjuntaba si la versión se creaba desde la web de GitHub o con una etiqueta sin `v` (como `1.1`). Ahora se genera con cualquier etiqueta.

## [1.1] - 2026-09-25

### Añadido
- Licencia MIT.
- `AudioTracker.exe` autónomo: la SoundFont va dentro del ejecutable, así que basta con descargarlo y hacer doble clic. Se adjunta solo a cada versión que se publica en *Releases*.

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
