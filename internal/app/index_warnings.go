package app

import "log"

// maxIndexWarnings is how many distinct index errors a run logs in full; further
// distinct errors are counted in the closing line.
const maxIndexWarnings = 5

// indexWarnings logs the errors the search index refuses a run's messages with
// (g7 friction re-verification of F7): the first occurrence of each distinct
// error as it happens, and, at the end, one line saying how many more messages
// failed the same way — instead of one identical line per message, thousands
// of them on a real run. Every refusal is still counted by the caller
// (INDEX-ERRORS), logged or not.
type indexWarnings struct {
	logger *log.Logger
	order  []string       // logged errors, in first-seen order
	seen   map[string]int // error text -> messages that met it
	hidden int            // messages whose distinct error was past maxIndexWarnings
}

func newIndexWarnings(logger *log.Logger) *indexWarnings {
	return &indexWarnings{logger: logger, seen: map[string]int{}}
}

// add records one refused message's error, logging it when it is new.
func (w *indexWarnings) add(err error) {
	msg := err.Error()
	if n, ok := w.seen[msg]; ok {
		w.seen[msg] = n + 1
		return
	}
	if len(w.order) >= maxIndexWarnings {
		w.hidden++
		return
	}
	w.seen[msg] = 1
	w.order = append(w.order, msg)
	w.logger.Printf("warning: index: %s", msg)
}

// flush logs the closing count lines for what add did not print. Safe to call
// more than once: it forgets what it reported.
func (w *indexWarnings) flush() {
	for _, msg := range w.order {
		if n := w.seen[msg]; n > 1 {
			w.logger.Printf("warning: index: the error above repeated for %d more message(s): %s", n-1, msg)
			w.seen[msg] = 1
		}
	}
	if w.hidden > 0 {
		w.logger.Printf("warning: index: %d more message(s) failed with other errors not shown (the first %d distinct errors are logged)", w.hidden, maxIndexWarnings)
		w.hidden = 0
	}
}
