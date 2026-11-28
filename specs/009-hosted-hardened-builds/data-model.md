# Data Model: Fase 009

## 1. Manifiesto

### Campos nuevos (todos los niveles)

```yaml
exclude:
  suites: {…}
  arches:                       # nuevo (constitución 1.9.0)
    arm64: "<motivo, ≥ 10 caracteres>"
signing:                        # nuevo; obligatorio si upstream firma (clarify)
  minisign:
    key: RWQlAjJC23149WL2sEpT/l0QKy7hMIFhYdQOFy0Z7z7PbneUgvlsnYcV
    file: https://release.files.ghostty.org/{{version}}/ghostty-{{version}}.tar.gz.minisig
  # o gpg: {fingerprint: <40 hex>, key_file: packages/<p>/upstream.asc, file: <url>}
  # o sigstore: {identity: <regex del certificado>, issuer: <url>, bundle: <url>}
```

Reglas: `exclude.arches` solo con arquitecturas de la configuración y al menos una sin excluir;
exactamente un tipo dentro de `signing`; las URLs de firma con las mismas plantillas y reglas de
origen que el artefacto (mismo release o repositorio upstream al tag).

### Nivel 2: toolchain, dependencias y excepciones

```yaml
source:
  url: https://release.files.ghostty.org/{{version}}/ghostty-{{version}}.tar.gz
  format: tar.gz
  extra_files:                  # archivos del tarball que no están (o difieren) en el tag
    - {path: "build.zig.zon.txt", reason: "lista generada al empaquetar"}
toolchain:
  name: zig
  version: 0.17.0
  url: https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz
  arch_map: {amd64: x86_64, arm64: aarch64}
  signing:
    minisign: {key: "<llave de Zig>", file: "https://ziglang.org/download/{{version}}/zig-{{arch}}-linux-{{version}}.tar.xz.minisig"}
deps:
  manager: zig                  # único valor en esta fase
  exclude_paths: [example/]     # árboles de upstream que no forman parte del build
  skip:                         # dependencias que el build no usa (FR-002): no van al fuente
    sentry: "sentry solo se activa en macOS e iOS"
```

### Excepciones de licencia (constitución 1.12.0, FR-027)

```yaml
license_exceptions:
  - dependency: freetype        # nombre en build.zig.zon; omitido = el propio paquete
    licenses: [GPL-2.0-or-later] # de allowed o de exception_only (config/licenses.yaml)
    file: docs/GPLv2.TXT        # opcional: texto dentro de la dependencia
    # repo_file: licenses/x.txt # o un texto guardado en packages/<p>/ si upstream no lo trae
    reason: "<motivo, ≥ 10 caracteres>"
```

Reglas: una excepción por dependencia; `file` y `repo_file` excluyentes y sin `..`; la del propio
paquete solo lleva `licenses` y `reason`; una excepción que no corresponde a ninguna dependencia
resuelta hace fallar el `lock`. El lockfile registra el `name` de cada dependencia.

`source.url` de nivel 2 admite, además de GitHub, el dominio de releases de upstream declarado en
`homepage` (caso ghostty), siempre con firma fijada.

## 2. Lockfile (`packages/<p>.lock.yaml`)

```yaml
versions:
  1.4.0:
    source_date_epoch: …
    license_sha256: …
    source_sha256: cb188ac6…             # 008
    tag_archive_sha256: …               # D9
    extra_files: {build.zig.zon.txt: <sha256>}
    toolchain: {amd64: <sha256>, arm64: <sha256>}
    deps:                               # D2, ordenadas por hash
      - {url: "https://deps.files.ghostty.org/uucode-….tar.gz", hash: "uucode-0.2.0-ZZjB…", sha256: "<árbol>"}
      - {url: "git+https://codeberg.org/ziglang/arocc#d0c8…", hash: "aro-0.0.0-JSD1…", sha256: "…"}
    zigdeps_sha256: <sha256 del componente .orig-zigdeps.tar.xz>
```

## 3. Paquete fuente de nivel 2 con dependencias

- `<p>_<upstream>.orig.tar.gz` (tarball de upstream, 008) y
  `<p>_<upstream>.orig-zigdeps.tar.xz` (D3): comunes a todas las suites.
- `debian/upstream/toolchain`: `name`, `version`, URL y sha256 por arquitectura.

## 4. Estado

- `Published.Arches []string` (`arches`, omitido si son todas las de la configuración): nivel 1 y
  nivel 2. `Validate`: `debs` (o `builds.<s>.debs`) tiene exactamente esas arquitecturas.
- Nivel 2: `Orig` admite componentes: `orig: {file, sha256, components: {zigdeps: {file, sha256}}}`.

## 5. Auditoría

- `extend` puede llevar `arches` (arquitecturas agregadas) además de `suites`.
- `publish`/`extend` de nivel 2 con toolchain registran `toolchain` (nombre, versión, sha256 por
  arquitectura) para trazabilidad.

## 6. Artefactos del run

| Artefacto | Productor | Contenido | Atestado |
|---|---|---|---|
| `source-<p>` | `level2-source` | `.orig*`, `.dsc` y `.debian.tar.xz` por suite, informe de cadena de suministro | sí |
| `build-<p>-<suite>-<arch>-<variant>` | `level2-build` | `.deb`, `.buildinfo`, `build.json` | sí |
| `l1-<p>-<arch>` | `l1-build` | `.deb` y `build.json` de nivel 1 | sí |
| `test-<p>-<suite>-<arch>` | `test` | `test.json` (sha256 del `.deb` probado, resultado) | sí |
| `review-<p>` | jobs de PR | Scorecard y resumen con IA | no (solo informativo) |

## 8. Procedencia publicada (`/<p>/<versión>/provenance.json`, research D18)

`{package, version, level, upstream: {urls, sha256, signature, provenance, tarball_vs_tag},
manifest_commit, run_url, attestations: [{subject, digest, rekor_url}], buildinfo: [urls],
tests: [{suite, arch, status}], audit_seq, verified_by_pc: {lintian, rebuild_level1, smoke_amd64}}`.
Se genera al publicar y forma parte del sitio; `debfactory verify` lo lee.

## 9. Ubicación de un paquete (research D21)

`packages/<p>/<p>.yaml`, `packages/<p>/<p>.lock.yaml`, `packages/<p>/debian/`; la ubicación
anterior (`packages/<p>.yaml`) se acepta durante la transición.

## 7. Informe de cadena de suministro (`supply.json`)

`{package, version, signature: {type, ok}, provenance: {present, ok, identity}, tarball_vs_tag:
{extra: [...], declared: [...]}, toolchain: {ok}, deps: {count, ok}, scorecard: {score, low: [...]}}`;
lo produce `level2-source` (nivel 2) o el build local (nivel 1) y lo resume el PR.
