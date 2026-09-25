# Cómo colaborar

## Preparar el entorno

```powershell
scripts\setup.ps1   # una vez: SoundFont y dependencias
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

1. Mueve lo de *Sin publicar* del CHANGELOG a una sección con número y fecha.
2. `git tag vX.Y.Z` y `git push --tags`.
