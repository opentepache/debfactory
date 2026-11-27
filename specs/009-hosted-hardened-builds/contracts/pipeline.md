# Contrato: compilación hospedada de nivel 2 y verificación en la PC

## Órdenes nuevas o ampliadas

```text
debfactory lock <pkg>                         # + toolchain, deps de Zig, tarball contra tag, firmas, procedencia
# Modo job hospedado (cada orden deja sus archivos en DIR para upload-artifact):
debfactory build <pkg> --arch A --out DIR     # nivel 1
debfactory level2 source <pkg> --out DIR      # en job hospedado (red): verifica todo y genera los paquetes fuente
debfactory level2 build <pkg> --suite S --arch A --variant V --source DIR --out DIR
                                              # en job hospedado: toolchain, build-deps, red cortada, dpkg-buildpackage
debfactory supply report <pkg>                # informe de cadena de suministro (también Scorecard)
debfactory test <pkg> --suite S --arch A --out DIR   # en job hospedado: prueba un .deb (008) y deja test.json
debfactory accept --artifacts DIR --repo R --workflow W --commit C
                                              # en la PC: verificación independiente (research D17) de todo el run
debfactory verify <pkg> <version> [--suite S --arch A]   # para terceros (research D18)
debfactory migrate layout                     # packages/<p>.yaml → packages/<p>/ (research D21)
```

`ci main` en la PC usa `accept` en vez de construir y probar; `ci main --local` (FR-011b) hace todo
en la PC como en la fase 008. `ci pr` deja de existir en la PC: las PR corren enteras en GitHub.

## Workflows de `packages` (los empuja el mantenedor)

```text
main.yml (push a main)
  plan        runs-on: ubuntu-24.04              matriz: paquetes afectados × suites × arquitecturas (y variantes de nivel 2)
  l1-build    runs-on: ubuntu-24.04              needs: plan       permissions: contents read, id-token write, attestations write
  l2-source   runs-on: ubuntu-24.04              needs: plan       mismos permisos
  l2-build    runs-on: ubuntu-24.04 | ubuntu-24.04-arm (matriz)  needs: l2-source
  test        runs-on: ubuntu-24.04 | ubuntu-24.04-arm (matriz)  needs: l1-build, l2-build
  publish     runs-on: [self-hosted, debfactory-main]  needs: test   descarga artefactos → debfactory accept → ci main
pr.yml (pull_request)
  plan, l1-build, l2-source, l2-build, test   igual que main.yml, sin publicar
  review      runs-on: ubuntu-24.04              permissions: contents read, models read
  comment     runs-on: ubuntu-24.04  needs: review, test   permissions: pull-requests write  (sin IA)
```

Cada job hospedado compila `debfactory` desde el commit de `debfactory.version` (repositorio público)
y comprueba ese commit antes de usarlo.

Ningún job hospedado recibe `secrets.*` ni `GITHUB_TOKEN` con escritura de contenido; una prueba de
la herramienta revisa los workflows (como `ci-lint` hoy) y falla si un job hospedado declara
secretos o permisos no listados aquí.

## Rechazos en `accept`

| Caso | Etapa | Resultado |
|---|---|---|
| artefacto sin atestación | `attest` | falla, nada se publica |
| atestación de otro repositorio, workflow, ref o commit | `attest` | falla |
| las dos variantes con sha256 distinto | `compare` | falla |
| falta una combinación (suite × arquitectura no excluida) o su informe de prueba | `inputs` | falla |
| el nivel 1 reconstruido en la PC no da el sha256 del artefacto | `compare` | falla |
| lintian o la prueba de humo amd64 fallan en la PC | `lintian` / `test` | falla |
| repositorio de paquetes no público | `config` | se niega a usar runners hospedados (FR-012) |
