# Quickstart: validar la fase 009

## 1. Medición previa (antes de implementar)

- Comparar el tarball de release de ghostty 1.4.0 con el archivo del tag `v1.4.0`: qué archivos
  trae de más o distintos (alimenta `source.extra_files`, research D9).
- Probar `cosign verify-blob-attestation` contra una atestación real de aqua y contra una propia de
  un workflow de prueba (research D6, D11).

## 2. Local

```sh
bin/debfactory lock --root testdata/project ghostty     # toolchain, 39 deps, tarball contra tag, firmas
bin/debfactory supply report --root testdata/project ghostty
just check
```

Esperado: lockfile con toolchain, `deps`, `tag_archive_sha256`, `zigdeps_sha256`; casos alterados
(dependencia, toolchain, tarball con archivo extra, firma con otra llave, procedencia ajena) fallan
antes de compilar (SC-003).

## 3. Runners hospedados (PR de prueba en `packages`)

Requisito: `opentepache/debfactory` público y la regla de commits firmados en `packages`.

1. PR con ghostty: `l2-source`, `l2-build` (8 combinaciones: trixie, forky × amd64, arm64 × 2) y
   `test` (4 combinaciones) corren en paralelo en runners estándar; `review` comenta Scorecard y el
   resumen. Ningún job usa runners propios.
2. Tiempo de reloj hasta tener todos los informes < 30 min (SC-002).
3. Integrar: el runner `pub` hace `debfactory accept` (atestaciones, doble compilación, lintian,
   reconstrucción del nivel 1, prueba de humo amd64) y publica.

## 4. Publicación y exclusión

- Integrar: `main` publica ghostty en trixie y forky (amd64, arm64); `apt install ghostty` y
  `ghostty +version` → `1.4.0` (SC-001).
- tepache-hello con `exclude.arches.arm64`: se publica solo amd64; al quitarlo, la ejecución
  siguiente agrega arm64 sin cambiar amd64 (SC-004).
- `just e2e` en verde y nivel 1 con los mismos sha256 (SC-006).

## 5. Transparencia

- La página `/ghostty/1.4.0-0~tepache1/` del sitio muestra la procedencia completa con enlaces que
  funcionan (commit, run, Rekor, `.buildinfo`).
- En un contenedor limpio sin credenciales: `debfactory verify ghostty 1.4.0-0~tepache1 --suite
  trixie --arch amd64` → verificado; con el `.deb` alterado → no verificado (SC-007).
- `ci doctor` confirma la regla de commits firmados y que no queda el runner `ci` (SC-008).
