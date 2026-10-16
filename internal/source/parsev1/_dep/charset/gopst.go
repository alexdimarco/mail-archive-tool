package charset

// Not part of go-message: the snapshot of the labels go-pst v6.0.2 adds to this table.
//
// In the build that wrote users' archives, go-message's charset table was extended
// at init by go-pst (pkg/charsets.go: ExtendCharsets over its nameMap, looking each
// goName up in x/net's html/charset, then in x/text's ianaindex) and again by
// openPST. This copy of the package sees neither, so the effective table is written
// out here, label by label, to the encoding each resolved to. A label whose lookup
// found nothing (cp857, cp858, cp864, cp869, cp932, cp950, roman8) was never
// registered and is absent. MA-331 checks this table against the shared one and
// against its recorded snapshot.

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

var goPSTLabels = map[string]encoding.Encoding{
	"utf8":     unicode.UTF8,
	"big5":     traditionalchinese.Big5,
	"big5hk":   traditionalchinese.Big5,
	"iso_1":    charmap.Windows1252,
	"cp874":    charmap.Windows874,
	"cp437":    charmap.CodePage437,
	"cp850":    charmap.CodePage850,
	"cp852":    charmap.CodePage852,
	"cp855":    charmap.CodePage855,
	"cp860":    charmap.CodePage860,
	"cp866":    charmap.CodePage866,
	"cp936":    simplifiedchinese.GBK,
	"cp1250":   charmap.Windows1250,
	"cp1251":   charmap.Windows1251,
	"cp1252":   charmap.Windows1252,
	"cp1253":   charmap.Windows1253,
	"cp1254":   charmap.Windows1254,
	"cp1255":   charmap.Windows1255,
	"cp1256":   charmap.Windows1256,
	"cp1257":   charmap.Windows1257,
	"cp1258":   charmap.Windows1258,
	"gb18030":  simplifiedchinese.GB18030,
	"greek8":   charmap.ISO8859_7,
	"iso88592": charmap.ISO8859_2,
	"iso88595": charmap.ISO8859_5,
	"iso88596": charmap.ISO8859_6,
	"iso88597": charmap.ISO8859_7,
	"iso88598": charmap.ISO8859_8,
	"iso88599": charmap.Windows1254,
	"iso15":    charmap.ISO8859_15,
	"koi8":     charmap.KOI8R,
	"sjis":     japanese.ShiftJIS,
	"tis-620":  charmap.Windows874,
}

func init() {
	for name, enc := range goPSTLabels {
		RegisterEncoding(name, enc)
	}
}
