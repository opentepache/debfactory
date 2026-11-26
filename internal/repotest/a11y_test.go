//go:build e2e

package repotest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"debfactory/internal/container"
	"debfactory/internal/site"
)

// A11yImage es la imagen de la auditoría de accesibilidad (SC-006): la de
// Playwright fijada por digest, con playwright y axe-core en versión exacta.
// Solo se usa en pruebas, en contenedor, sin credenciales; no hay un auditor
// WCAG empaquetado en Debian (plan.md § Complexity Tracking).
const A11yImage = "debfactory-a11y"

const a11yDockerfile = `FROM mcr.microsoft.com/playwright@sha256:70e367e0cbf60340a5b5fd562f6247a34eb3196efab9f88a3dd56482d9fe09d2
# v1.49.1-noble
WORKDIR /a11y
RUN npm init -y >/dev/null && npm install --no-audit --no-fund --save-exact playwright@1.49.1 axe-core@4.10.2
ENV NODE_PATH=/a11y/node_modules
`

// a11yScript abre cada página a 320 y 1280 px, en claro y en oscuro, ejecuta
// axe con las reglas WCAG 2.1 A y AA y comprueba que el documento no se
// desplaza en horizontal (FR-019, FR-020).
const a11yScript = `const { chromium } = require('playwright');
const fs = require('fs');
const axeSrc = fs.readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8');
(async () => {
  const urls = process.argv.slice(2);
  const browser = await chromium.launch();
  let failures = 0;
  for (const width of [320, 1280]) {
    for (const colorScheme of ['light', 'dark']) {
      const ctx = await browser.newContext({ viewport: { width, height: 800 }, colorScheme });
      const page = await ctx.newPage();
      for (const url of urls) {
        await page.goto(url, { waitUntil: 'load' });
        await page.addScriptTag({ content: axeSrc });
        const r = await page.evaluate(() => axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } }));
        for (const v of r.violations) {
          failures++;
          console.log(url + ' @' + width + '/' + colorScheme + ': ' + v.id + ' (' + v.impact + '): ' + v.help + ': ' +
            v.nodes.slice(0, 3).map(n => n.target.join(' ')).join(', '));
        }
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        if (overflow > 0) {
          failures++;
          console.log(url + ' @' + width + ': el documento se desplaza ' + overflow + ' px en horizontal');
        }
      }
      await ctx.close();
    }
  }
  await browser.close();
  console.log('a11y: ' + urls.length + ' páginas, ' + failures + ' problemas');
  process.exit(failures ? 1 : 0);
})().catch(e => { console.error(e); process.exit(2); });
`

// TestE2EA11y audita todas las páginas de los dos idiomas servidas como en
// el hosting (SC-006).
func TestE2EA11y(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	_, www, _ := serveSite(t, ctx)
	if err := container.EnsureImage(ctx, A11yImage, a11yDockerfile, "", 30*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	files, err := site.Files(www)
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, f := range files {
		if strings.HasSuffix(f, "/index.html") || f == "index.html" {
			if strings.HasPrefix(f, "dists/") || strings.HasPrefix(f, "pool/") {
				continue
			}
			urls = append(urls, TestURL+"/"+strings.TrimSuffix(f, "index.html"))
		}
	}
	if len(urls) < 4 {
		t.Fatalf("pocas páginas: %v", urls)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a11y.js"), []byte(a11yScript), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := container.Run(ctx, container.Opts{
		Image:   A11yImage,
		Network: Network,
		Mounts:  []container.Mount{{Src: dir, Dst: "/check", ReadOnly: true}},
		Cmd:     append([]string{"node", "/check/a11y.js"}, urls...),
	})
	out := string(res.Stdout) + string(res.Stderr)
	t.Log(out)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("auditoría de accesibilidad: código %d, %v", res.ExitCode, err)
	}
}
