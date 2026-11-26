# Data Model: Suites configurables y Debian forky

## 1. Suite (`config/debfactory.yaml`, bloque `suites`)

```yaml
suites:            # orden = orden del informe y del sitio
  bookworm: {image: "debian:bookworm", level2_suffix: deb12, distro: debian, label: "Debian 12", version: "12"}
  trixie:   {image: "debian:trixie",   level2_suffix: deb13, distro: debian, label: "Debian 13", version: "13"}
  forky:    {image: "debian:forky",    level2_suffix: deb14, distro: debian, label: "Debian 14 (testing)", version: "14"}
  sid:      {image: "debian:sid",      level2_suffix: debsid, distro: debian, label: "Debian sid", version: "sid", catchall: true}
  noble:    {image: "ubuntu:noble",    level2_suffix: u2404, distro: ubuntu, label: "Ubuntu 24.04", version: "24.04"}
  resolute: {image: "ubuntu:resolute", level2_suffix: u2604, distro: ubuntu, label: "Ubuntu 26.04", version: "26.04"}
```

| Campo | Reglas |
|---|---|
| nombre (clave) | codename; `[a-z][a-z0-9-]*` |
| `image` | obligatorio (como hoy) |
| `level2_suffix` | obligatorio y único entre las suites (constitución VI) |
| `distro` | obligatorio: `debian` o `ubuntu` |
| `label` | obligatorio, una línea |
| `version` | obligatorio, una línea, sin espacios; versión corta ("14", "24.04"). En `site.json` se llama igual (antes `short`) |
| `catchall` | opcional, booleano; a lo sumo una suite por `distro` |

Errores: `suites.forky.label: obligatorio`, `suites.x.distro: "arch" no es debian ni ubuntu`,
`suites: más de una suite catchall en debian (sid, forky)`.

## 2. Versión publicada (`repo/state.yaml`, sin cambio de formato)

Una ampliación cambia solo `suites` de la versión más alta de un paquete: agrega las suites nuevas
en el orden de la configuración. `version`, `published` y `debs` no cambian (research D2).

## 3. Plan de `ci main`

Lista nueva `extend` con los paquetes a ampliar:

```json
{"extend": [{"package": "bob", "version": "4.3.0-0~tepache1", "commit": "<HEAD>", "reason": "extend",
             "suites": ["forky"]}]}
```

`suites` son las que le faltan (configuradas, no excluidas por el manifiesto de `HEAD`, no
publicadas). Un paquete está en `extend` solo si la versión del manifiesto es la más alta publicada.

## 4. Entrada de auditoría `extend` (`repo/audit.jsonl`)

```json
{"seq":6,"time":"…","op":"extend","package":"bob","version":"4.3.0-0~tepache1","suites":["forky"],
 "commit":"…","tool":"…","debs":{"amd64":"…","arm64":"…"},"serial":3,"run":"https://…"}
```

`suites` va después de `version` en el orden fijo de las claves. Solo aparece en `extend`.
