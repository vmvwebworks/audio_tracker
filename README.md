# Audio Tracker

App de escritorio para Windows que reproduce partituras de **Guitar Pro 7/8** (`.gp`) mostrando la tablatura, mientras **grabas tu instrumento** por ASIO. Puede sonar con el sintetizador o sobre el **tema real** exportado de Reaper, con la partitura sincronizada.

## Descargar y usar

1. Descarga **`AudioTracker.exe`** de la [última versión](https://github.com/vmvwebworks/audio_tracker/releases/latest).
2. Haz doble clic. No hay que instalar nada: los sonidos van dentro del programa.

Solo necesitas Windows 10/11 de 64 bits y un driver ASIO para tu tarjeta de sonido (si no tiene uno propio, [ASIO4ALL](https://asio4all.org)).
La primera vez Windows puede avisar de que es una aplicación desconocida: pulsa *Más información* → *Ejecutar de todas formas*.

Cómo se usa: [docs/USO.md](docs/USO.md).

## Desarrollo

Está escrita en Go puro, sin cgo: con [Go](https://go.dev/dl/) (la versión de `go.mod`) basta, sin compilador de C. La SoundFont está en el repositorio y se incluye en el ejecutable, así que `go build` ya genera un `.exe` completo.

```powershell
git clone git@github.com:vmvwebworks/audio_tracker.git
cd audio_tracker
scripts\check.ps1          # formato, vet, tests y compilación
scripts\run.ps1            # compila y abre la app
scripts\run.ps1 -Sandbox   # igual, con configuración temporal y partitura de prueba
scripts\release.ps1        # genera dist\AudioTracker.exe, el que se publica
```

Si PowerShell bloquea los scripts: `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`. Al subir una etiqueta `vX.Y.Z`, GitHub compila y publica el `.exe` solo (ver [CONTRIBUTING.md](CONTRIBUTING.md#publicar-una-versión)).

## Documentación

| Documento | Para qué |
|---|---|
| [docs/USO.md](docs/USO.md) | Manual de uso de la app |
| [docs/ARQUITECTURA.md](docs/ARQUITECTURA.md) | Cómo está hecha: paquetes, hilos, líneas de tiempo, archivos |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Cómo colaborar: flujo, comprobaciones y qué documentar |
| [AGENTS.md](AGENTS.md) | Instrucciones para agentes de IA (Claude Code, Codex, Copilot…) |
| [CHANGELOG.md](CHANGELOG.md) | Qué ha cambiado en cada versión |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Ideas y pendientes |

## Créditos

- Interfaz: [Gio](https://gioui.org). Síntesis: [go-meltysynth](https://github.com/sinshu/go-meltysynth). MP3: [go-mp3](https://github.com/hajimehoshi/go-mp3).
- Sonidos: SoundFont [GeneralUser GS](https://www.schristiancollins.com/generaluser.php) de S. Christian Collins, incluida en `assets/` y en el ejecutable (su licencia permite usarla en programas).
- Guitar Pro es una marca de Arobas Music; este proyecto no está afiliado a ella.

## Licencia

[MIT](LICENSE). La SoundFont GeneralUser GS tiene su propia licencia: consúltala en [su repositorio](https://github.com/mrbumpy409/GeneralUser-GS).
