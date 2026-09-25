# Datos de prueba

`minimal.gpif` es una partitura Guitar Pro 7/8 mínima escrita a mano (el contenido de `Content/score.gpif` dentro de un `.gp`). Los tests la empaquetan con `testutil.FixtureGP`, y `scripts\run.ps1 -Sandbox` la abre en la app.

Contiene: guitarra (afinación estándar) y batería, 5 compases con repetición de los compases 2 y 3 (orden 1 2 3 2 3 4 5), un 3/4, la sección "Estribillo", una ligadura y tempo 100.

Si lo modificas, actualiza las cifras esperadas en `internal/gp/gp_test.go`.
