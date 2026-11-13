package sync

import (
	"context"
	"net/http"
	"testing"
	"time"

	"debfactory/internal/fetch"
)

// staleFetcher sirve old las primeras n veces y luego new.
type staleFetcher struct {
	old, new []byte
	n, calls int
}

func (f *staleFetcher) get(context.Context, string) ([]byte, http.Header, error) {
	f.calls++
	if f.calls <= f.n {
		return f.old, nil, nil
	}
	return f.new, nil, nil
}

func TestGetFresh(t *testing.T) {
	defer func(r time.Duration) { staleRetry = r }(staleRetry)
	staleRetry = time.Millisecond
	want := fetch.SHA256([]byte("gen-2"))

	// La CDN sirve la generación anterior dos veces y luego la nueva.
	f := &staleFetcher{old: []byte("gen-1"), new: []byte("gen-2"), n: 2}
	stale := 0
	data, _, err := getFresh(context.Background(), f, "dists/trixie/InRelease", want,
		time.Now().Add(time.Minute), func() { stale++ })
	if err != nil || string(data) != "gen-2" || f.calls != 3 || stale != 2 {
		t.Fatalf("data=%q err=%v calls=%d stale=%d", data, err, f.calls, stale)
	}

	// Si nunca llega, se rinde al pasar el plazo y devuelve lo último.
	f = &staleFetcher{old: []byte("gen-1"), n: 1 << 30}
	data, _, err = getFresh(context.Background(), f, "dists/trixie/InRelease", want,
		time.Now().Add(20*time.Millisecond), func() {})
	if err != nil || string(data) != "gen-1" || f.calls < 2 {
		t.Fatalf("data=%q err=%v calls=%d", data, err, f.calls)
	}
}
