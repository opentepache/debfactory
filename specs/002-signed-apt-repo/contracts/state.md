# Contrato: `repo/state.yaml`

Lo escriben solo `publish`, `retire` y `repo rebuild`. La escritura es atómica (`.tmp` y luego
renombrar) y determinista:

- claves en orden fijo;
- paquetes en orden alfabético;
- versiones de mayor a menor según `dpkg --compare-versions`;
- `retired` en orden de fecha.

El diff se revisa en git, igual que los lockfiles. La lectura es estricta: rechaza campos
desconocidos.

```yaml
# Generado por debfactory publish/retire. No editar a mano.
schema: 1
serial: 7
packages:
  bob:
    - version: 4.3.0-0~tepache1
      published: 2026-11-20T10:02:11Z
      suites: [trixie, sid, noble, resolute]
      debs:
        amd64: 3f11f9ac1491942250b382f211a446bda8d20ec59dd27a9899671e41cce81805
        arm64: 9a0c…
  fnm:
    - version: 1.40.0-0~tepache1
      published: 2026-12-01T09:00:00Z
      suites: [bookworm, trixie, sid, noble, resolute]
      debs: {amd64: …, arm64: …}
    - version: 1.39.0-0~tepache1
      published: 2026-11-20T10:02:11Z
      suites: [bookworm, trixie, sid, noble, resolute]
      debs: {amd64: 83ed64394a86…, arm64: 8c96c7a3…}
retired:
  - package: fnm
    version: 1.40.1-0~tepache1
    date: 2026-12-03T18:30:00Z
    reason: "segfault al iniciar en arm64 (issue #12)"
```

## Reglas de validación (fallo = código 1, etapa `state`)

| Regla | Mensaje |
|---|---|
| `schema` distinto de 1 | `repo/state.yaml: schema 2 no soportado` |
| sha256 mal formado | `repo/state.yaml: packages.fnm[0].debs.amd64: sha256 inválido` |
| falta una arquitectura | `repo/state.yaml: packages.fnm[0].debs: falta arm64` |
| suite desconocida | `repo/state.yaml: packages.fnm[0].suites: suite "focal" no está en config` |
| más de `keep` versiones por suite | `repo/state.yaml: fnm tiene 4 versiones en trixie (keep: 3)` |
| una versión aparece publicada y retirada | `repo/state.yaml: fnm 1.40.1-0~tepache1 está publicada y retirada` |
