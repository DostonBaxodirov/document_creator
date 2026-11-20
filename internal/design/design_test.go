package design

import (
	"slices"
	"testing"
)

// TestListsHaveNoDuplicates — har bir ro'yxatda takroriy qiymat yo'q.
func TestListsHaveNoDuplicates(t *testing.T) {
	lists := map[string][]string{
		"AllLayouts": AllLayouts, "CoreLayouts": CoreLayouts, "SignatureLayouts": SignatureLayouts,
		"Covers": Covers, "Headers": Headers, "Cards": Cards, "Corners": Corners, "Markers": Markers,
		"Motifs": Motifs, "Sides": Sides, "HeadingFonts": HeadingFonts, "BodyFonts": BodyFonts,
		"DocTitles": DocTitles, "DocHeadings": DocHeadings, "DocBorders": DocBorders,
		"DocOrnaments": DocOrnaments, "DocHeadingFonts": DocHeadingFonts,
	}
	for name, list := range lists {
		if len(list) == 0 {
			t.Errorf("%s bo'sh", name)
		}
		seen := make(map[string]bool, len(list))
		for _, v := range list {
			if seen[v] {
				t.Errorf("%s: %q takrorlangan", name, v)
			}
			seen[v] = true
		}
	}
}

// TestLayoutSubsets — asosiy va imzo maketlari barcha maketlar ichida.
func TestLayoutSubsets(t *testing.T) {
	for name, list := range map[string][]string{"CoreLayouts": CoreLayouts, "SignatureLayouts": SignatureLayouts} {
		for _, l := range list {
			if !slices.Contains(AllLayouts, l) {
				t.Errorf("%s: %q AllLayouts da yo'q", name, l)
			}
		}
	}
}

func TestParseHex(t *testing.T) {
	for in, want := range map[string]string{"#1a2B3c": "1A2B3C", "abc": "AABBCC", " #FFF ": "FFFFFF"} {
		if got, ok := ParseHex(in); !ok || got != want {
			t.Errorf("ParseHex(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "#12", "#1234567", "red", "#GGGGGG"} {
		if _, ok := ParseHex(in); ok {
			t.Errorf("ParseHex(%q) xato berishi kerak", in)
		}
	}
}
