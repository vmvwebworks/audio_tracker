# Audio Tracker

App de escritorio para Windows que reproduce partituras de **Guitar Pro 7/8** (`.gp`) mostrando la tablatura, mientras **grabas tu instrumento** por ASIO. Puede sonar con el sintetizador o sobre el **tema real** exportado de Reaper, con la partitura sincronizada.

Está escrita en Go puro, sin cgo: `go build` basta, sin compilador de C.

## Empezar

Requisitos: Windows 10/11 de 64 bits, [Go](https://go.dev/dl/) (la versión que indica `go.mod`) y un driver ASIO (por ejemplo [ASIO4ALL](https://asio4all.org)).

```powershell
git clone git@github.com:vmvwebworks/audio_tracker.git
cd audio_tracker
scripts\setup.ps1          # descarga la SoundFont (32 MB) y las dependencias
scripts\check.ps1          # formato, vet, tests y compilación
scripts\run.ps1            # compila y abre la app
scripts\run.ps1 -Sandbox   # igual, con configuración temporal y partitura de prueba
```

Si PowerShell bloquea los scripts: `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`.

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
- Sonidos: SoundFont [GeneralUser GS](https://github.com/mrbumpy409/GeneralUser-GS) de S. Christian Collins. No se incluye en el repositorio: la descarga `scripts\setup.ps1`.
- Guitar Pro es una marca de Arobas Music; este proyecto no está afiliado a ella.

## Licencia

[MIT](LICENSE). La SoundFont GeneralUser GS tiene su propia licencia: consúltala en [su repositorio](https://github.com/mrbumpy409/GeneralUser-GS).
