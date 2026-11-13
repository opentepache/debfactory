package report

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// TestBufferedNoInterleave: los bloques de tareas en paralelo salen enteros.
func TestBufferedNoInterleave(t *testing.T) {
	var out, errb bytes.Buffer
	p := &Printer{Out: &out, Err: &errb}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child, flush := p.Buffered()
			for range 50 {
				child.Line("tarea %02d", i)
			}
			flush()
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1000 {
		t.Fatalf("%d líneas", len(lines))
	}
	for i := 0; i < len(lines); i += 50 {
		for _, l := range lines[i : i+50] {
			if l != lines[i] {
				t.Fatalf("bloque mezclado en la línea %d: %q después de %q", i, l, lines[i])
			}
		}
	}
}
