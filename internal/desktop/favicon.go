package desktop

import (
	_ "embed"
	"encoding/base64"
)

// faviconPNG is MailArchive's icon at 32 px, the small drawing also in the .ico and
// the .icns. packaging/icon/pack writes this file from packaging/icon/png (MA-1077).
//
//go:embed favicon.png
var faviconPNG []byte

// faviconLink is the icon link every Desktop page carries in its head (MA-1081). The
// PNG is inline as a data: URL, which the pages' Content-Security-Policy already allows
// (img-src data:), so the browser shows it without fetching anything.
var faviconLink = `<link rel="icon" type="image/png" href="data:image/png;base64,` +
	base64.StdEncoding.EncodeToString(faviconPNG) + `">`
