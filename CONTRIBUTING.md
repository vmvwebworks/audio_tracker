# Cómo colaborar

## Preparar el entorno

```powershell
scripts\setup.ps1   # opcional: comprueba Go y la SoundFont, y descarga los módulos
scripts\check.ps1   # antes de cada commit: formato, vet, tests, compilación
```

## Flujo de trabajo

1. Crea una rama desde `main`: `feat/<algo>`, `fix/<algo>` o `docs/<algo>`.
2. Haz el cambio y **su documentación en el mismo PR** (ver abajo).
3. Pasa `scripts\check.ps1` y prueba la app con `scripts\run.ps1 -Sandbox`.
4. Abre un PR hacia `main`. La plantilla trae la lista de comprobación. El CI repite las comprobaciones y exige que se actualice `CHANGELOG.md` cuando cambia el código.

`main` está protegida: GitHub rechaza los pushes directos, y un PR solo se puede fusionar con los controles `check` y `changelog` en verde. Los PR se fusionan con *squash* (un commit por PR) y la rama se borra sola.

Commits pequeños y con mensaje claro, en español o en inglés. Formato recomendado: `feat: tomas favoritas`, `fix: cursor en la intro`.

## Qué documentar en cada cambio

| Si tu cambio… | Actualiza |
|---|---|
| Añade o cambia algo que nota el usuario | `CHANGELOG.md` (sección *Sin publicar*) y `docs/USO.md` |
| Añade un paquete, un hilo, un archivo persistente o cambia una regla de diseño | `docs/ARQUITECTURA.md` |
| Añade un comando, script o paso de preparación | `README.md` (Empezar) y `AGENTS.md` |
| Completa o descarta algo del roadmap | `docs/ROADMAP.md` |
| Arregla un fallo interno sin efecto visible | `CHANGELOG.md` en *Corregido* (una línea) |

Criterio: la documentación describe cómo está el proyecto **ahora**, no la historia (para eso está el CHANGELOG). Si una frase deja de ser cierta, cámbiala; no añadas una nueva al lado.

## Tests

- `go test ./...` usa la partitura mínima `internal/gp/testdata/minimal.gpif` y no necesita hardware.
- Con `GP_SAMPLE=<ruta a un .gp>` los tests de motor usan esa partitura. Con `MP3_SAMPLE=<ruta>` se prueba el decodificador MP3.
- Todo lo que toque el hilo de audio necesita un test en `internal/engine` que llame a `e.process` en modo offline (mira `offline()` en `engine_test.go`).
- Si cambias el parser, amplía `minimal.gpif` con el caso nuevo y compruébalo en `internal/gp/gp_test.go`.

## Probar con hardware

`go run ./cmd/asiotest <archivo.gp> <carpeta>` reproduce y graba 3 segundos con el driver real, con todas las pistas en silencio, y muestra la latencia y si se han perdido muestras.

## Publicar una versión

1. En un PR, mueve lo de *Sin publicar* del CHANGELOG a una sección con número y fecha, y fusiónalo.
2. Crea la versión, de una de estas dos formas (el nombre de la etiqueta es libre: `1.2`, `v1.2`…):
   - En la web: *Releases* → *Draft a new release*, etiqueta nueva sobre `main`, *Publish release*.
   - Con git, desde `main` actualizado: `git tag 1.2` y `git push origin 1.2` (la versión se crea sola).
3. El flujo *Release* de GitHub pasa las comprobaciones, genera `AudioTracker.exe` (con `scripts\release.ps1`) y lo adjunta a la versión en [Releases](https://github.com/vmvwebworks/audio_tracker/releases). Tarda unos 3 minutos: hasta entonces la versión solo muestra el código fuente. Ese es el enlace que se pasa a los usuarios.

Para probar el ejecutable de usuario en local: `scripts\release.ps1` y abre `dist\AudioTracker.exe`.
