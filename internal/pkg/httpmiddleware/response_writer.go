package httpmiddleware

import "net/http"

// PreserveFlusher wraps w so outer middleware still satisfies http.Flusher when the
// underlying ResponseWriter supports streaming (SSE).
func PreserveFlusher(w http.ResponseWriter) http.ResponseWriter {
	if _, ok := w.(http.Flusher); ok {
		return w
	}
	if f, ok := unwrapFlusher(w); ok {
		return &flushWriter{ResponseWriter: w, flusher: f}
	}
	return w
}

type flushWriter struct {
	http.ResponseWriter
	flusher http.Flusher
}

func (w *flushWriter) Flush() {
	w.flusher.Flush()
}

type responseWriterUnwrapper interface {
	Unwrap() http.ResponseWriter
}

func unwrapFlusher(w http.ResponseWriter) (http.Flusher, bool) {
	for current := w; current != nil; {
		if f, ok := current.(http.Flusher); ok {
			return f, true
		}
		unwrapper, ok := current.(responseWriterUnwrapper)
		if !ok {
			break
		}
		current = unwrapper.Unwrap()
	}
	return nil, false
}
