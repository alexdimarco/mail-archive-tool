package index

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
)

// covers: MA-490, R8
// A mail is read the same at every depth: below the tree parser's 512-element
// cap by the tree walk, past it by the fallback, and across the band (from
// 480) where an exporter may flatten or refuse it. For block-spaced text, a
// one-line-per-level reply chain and the RV-3 payload (with its `</p>` and
// `</br>` variants), the text 470 to 600 levels deep equals the text at
// depth zero, so no depth changes which words the index holds or glues two
// of them together. The sweep is proven to straddle the cap.
func TestBodyTextSameWordsAcrossTheDepthCap(t *testing.T) {
	var chain strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&chain, "<div><p>chainline%d</p>", i)
	}
	chain.WriteString(strings.Repeat("</div>", 40))
	payloads := []string{
		`<p>alpha</p><p>bravo</p><div>charlie</div>delta<br>echo<li>foxtrot</li><table><tr><td>golf</td><td>hotel</td></tr></table>`,
		chain.String(),
		`<p>Hello</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account sweepkestrel today</p>`,
		`<p>Hello</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to sweepbrp today</p>--></style></svg><p>tail</p>`,
		`<p>Hello</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to sweepbrbr today</p>--></style></svg><p>tail</p>`,
	}
	depths := []int{470, 480, 490, 500, 505, 508, 509, 510, 511, 512, 520, 600}
	parsed, refused := 0, 0
	for _, p := range payloads {
		want := htmlText(p)
		assure.Reached(t, want, "text at depth zero")
		for _, d := range depths {
			h := deepWrap(d, p)
			if _, err := html.Parse(strings.NewReader(h)); err == nil {
				parsed++
			} else {
				refused++
			}
			if got := htmlText(h); got != want {
				t.Errorf("depth %d reads differently from depth zero for %q:\n zero  %q\n deep  %q", d, clipText(p), clipText(want), clipText(got))
			}
		}
	}
	assure.Reached(t, parsed, "sweep inputs the tree parser reads")
	assure.Reached(t, refused, "sweep inputs past the tree parser's cap")
}
