// Package logx is a tiny leveled JSON logger with an in-memory ring
// buffer. The ring is what the web UI will stream over SSE later; keeping
// it here means the whole app logs through one path.
package logx

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

var (
	mu      sync.Mutex
	level   = InfoLevel
	ring    = make([]map[string]any, 0, ringMax)
	ringMax = 1000
	subs    []chan map[string]any
)

func Init(lvl string) {
	switch lvl {
	case "debug":
		level = DebugLevel
	case "warn":
		level = WarnLevel
	case "error":
		level = ErrorLevel
	default:
		level = InfoLevel
	}
}

func Debug(msg string, kv ...any) { emit(DebugLevel, "debug", msg, kv...) }
func Info(msg string, kv ...any)  { emit(InfoLevel, "info", msg, kv...) }
func Warn(msg string, kv ...any)  { emit(WarnLevel, "warn", msg, kv...) }
func Error(msg string, kv ...any) { emit(ErrorLevel, "error", msg, kv...) }

func emit(l Level, lvl, msg string, kv ...any) {
	if l < level {
		return
	}
	e := map[string]any{"ts": time.Now().Format(time.RFC3339), "lvl": lvl, "msg": msg}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		if err, ok := v.(error); ok {
			v = err.Error() // errors have no exported fields; log the message
		}
		e[fmt.Sprint(kv[i])] = v
	}
	line, _ := json.Marshal(e)

	mu.Lock()
	ring = append(ring, e)
	if len(ring) > ringMax {
		ring = ring[len(ring)-ringMax:]
	}
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	mu.Unlock()

	os.Stdout.Write(append(line, '\n'))
}

// Subscribe returns a channel plus an idempotent cleanup function. Callers
// must unsubscribe when an SSE client disconnects so abandoned channels do
// not accumulate indefinitely.
func Subscribe() (<-chan map[string]any, func()) {
	ch := make(chan map[string]any, 256)
	mu.Lock()
	subs = append(subs, ch)
	mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			for i, sub := range subs {
				if sub == ch {
					subs = append(subs[:i], subs[i+1:]...)
					return
				}
			}
		})
	}
}

// Recent returns the last n ring entries (for the future SSE log stream).
func Recent(n int) []map[string]any {
	mu.Lock()
	defer mu.Unlock()
	if n <= 0 || n > len(ring) {
		n = len(ring)
	}
	out := make([]map[string]any, n)
	copy(out, ring[len(ring)-n:])
	return out
}
