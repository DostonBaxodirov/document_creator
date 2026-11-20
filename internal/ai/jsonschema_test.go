package ai

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"telegram-presentation-bot/internal/design"
)

// Sxemadan qism olish uchun test yordamchilari (topilmasa — nil).

func schemaProps(s map[string]any) map[string]any {
	p, _ := s["properties"].(map[string]any)
	return p
}

func schemaProp(s map[string]any, key string) map[string]any {
	p, _ := schemaProps(s)[key].(map[string]any)
	return p
}

func schemaItems(s map[string]any) map[string]any {
	i, _ := s["items"].(map[string]any)
	return i
}

func schemaRequired(s map[string]any) []string {
	r, _ := s["required"].([]string)
	return r
}

func schemaEnum(s map[string]any) []string {
	e, _ := s["enum"].([]string)
	return e
}

// slideItem taqdimot sxemasidagi bitta slayd sxemasi.
func slideItem(s map[string]any) map[string]any { return schemaItems(schemaProp(s, "slides")) }

// allSchemas — AI'ga yuboriladigan barcha sxemalar (barcha variantlari bilan).
func allSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"deck":                deckSchema(false, design.AllLayouts),
		"deck_images":         deckSchema(true, design.AllLayouts),
		"slide":               slideSchema(false, design.AllLayouts),
		"slide_images":        slideSchema(true, design.AllLayouts),
		"slides_batch":        slidesBatchSchema(false, design.AllLayouts),
		"slides_batch_images": slidesBatchSchema(true, design.AllLayouts),
		"deck_outline":        deckOutlineSchema(false, design.AllLayouts),
		"deck_outline_images": deckOutlineSchema(true, design.AllLayouts),
		"outline":             outlineSchema(false),
		"outline_figures":     outlineSchema(true),
		"section":             sectionSchema(),
		"design":              designSchema(),
		"doc_design":          docDesignSchema(),
	}
}

// walkSchema sxemadagi har bir tugunni (xossalar va massiv elementlari) aylanib chiqadi.
func walkSchema(path string, s map[string]any, visit func(path string, s map[string]any)) {
	visit(path, s)
	for k, p := range schemaProps(s) {
		sub, ok := p.(map[string]any)
		if !ok {
			visit(path+"."+k+" (map emas)", nil)
			continue
		}
		walkSchema(path+"."+k, sub, visit)
	}
	if items := schemaItems(s); items != nil {
		walkSchema(path+"[]", items, visit)
	}
}

// TestStrictSchemas — OpenAI strict rejimi talabi: har bir obyekt barcha
// xossalarini "required" da sanaydi va "additionalProperties": false.
func TestStrictSchemas(t *testing.T) {
	for name, s := range allSchemas() {
		if s["type"] != "object" {
			t.Errorf("%s: ildiz obyekt bo'lishi kerak: %v", name, s["type"])
		}
		objects := 0
		walkSchema(name, s, func(path string, n map[string]any) {
			if n == nil {
				t.Errorf("%s: xossa sxemasi map[string]any emas", path)
				return
			}
			if _, ok := n["type"].(string); !ok {
				t.Errorf("%s: type yo'q", path)
			}
			if n["type"] == "array" && schemaItems(n) == nil {
				t.Errorf("%s: massivda items yo'q", path)
			}
			if n["type"] != "object" {
				return
			}
			objects++
			props := schemaProps(n)
			if len(props) == 0 {
				t.Errorf("%s: obyektda xossalar yo'q", path)
			}
			req, ok := n["required"].([]string)
			if !ok {
				t.Errorf("%s: required []string emas: %T", path, n["required"])
			}
			keys := sortedKeys(props)
			sortedReq := slices.Sorted(slices.Values(req))
			if !slices.Equal(keys, sortedReq) {
				t.Errorf("%s: required %v, xossalar %v", path, req, keys)
			}
			if ap, ok := n["additionalProperties"].(bool); !ok || ap {
				t.Errorf("%s: additionalProperties false bo'lishi kerak: %v", path, n["additionalProperties"])
			}
		})
		if objects == 0 {
			t.Errorf("%s: obyekt topilmadi", name)
		}
	}
}

// TestSchemaPropertyOrder — xossalar tartibi saqlanadi: "required" da, OpenAI
// JSON'ida va Gemini'ning propertyOrdering'ida.
func TestSchemaPropertyOrder(t *testing.T) {
	s := Object(map[string]any{
		"z":     String(),
		"b":     Integer(),
		"a":     Array(Enum([]string{"x", "y"})),
		"extra": String(),
	}, "z", "b", "a", "nope", "z")
	if got := schemaRequired(s); !slices.Equal(got, []string{"z", "b", "a", "extra"}) {
		t.Fatalf("required = %v (keys tartibi, keyin qolganlari; yo'q va takroriy kalitlar tashlanadi)", got)
	}

	raw, err := marshalSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"properties":{"z":{"type":"string"},"b":{"type":"integer"},"a":{"items":{"enum":["x","y"],"type":"string"},"type":"array"},"extra":`) {
		t.Errorf("OpenAI sxemasida xossalar tartibi saqlanmadi: %s", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("sxema yaroqli JSON emas: %v\n%s", err, raw)
	}

	// Ichki obyektlarda ham tartib saqlanadi.
	raw, err = marshalSchema(deckSchema(true, design.AllLayouts))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"properties":{"title":{"type":"string"},"subtitle":{"type":"string"},"slides":`,
		`"required":["layout","title","lead","items","highlight","image"]`,
		`"properties":{"layout":{"enum":[`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("deck sxemasida %s yo'q:\n%s", want, raw)
		}
	}

	g := geminiSchema(deckOutlineSchema(true, design.AllLayouts)).(map[string]any)
	if got := g["propertyOrdering"]; !slices.Equal(got.([]string), []string{"title", "subtitle", "slides", "image"}) {
		t.Errorf("Gemini propertyOrdering = %v", got)
	}
	if got := slideItem(g)["propertyOrdering"]; !slices.Equal(got.([]string), []string{"n", "title", "layout", "focus", "image"}) {
		t.Errorf("Gemini slayd propertyOrdering = %v", got)
	}
}

// TestGeminiSchemaCopies — Gemini moslamasi asl sxemani o'zgartirmaydi.
func TestGeminiSchemaCopies(t *testing.T) {
	s := deckSchema(false, design.AllLayouts)
	g := geminiSchema(s).(map[string]any)
	if _, ok := s["propertyOrdering"]; ok {
		t.Error("asl sxemaga propertyOrdering qo'shilmasligi kerak")
	}
	if _, ok := slideItem(s)["propertyOrdering"]; ok {
		t.Error("asl ichki sxemaga propertyOrdering qo'shilmasligi kerak")
	}
	schemaRequired(g)[0] = "o'zgardi"
	if schemaRequired(s)[0] != "title" {
		t.Error("nusxaning required ro'yxati asl sxema bilan umumiy bo'lmasligi kerak")
	}
}

func TestSchemaName(t *testing.T) {
	for op, want := range map[string]string{
		"presentation_slides":   "presentation_slides",
		"doc-design":            "doc-design",
		"bo'lim 1":              "bo_lim_1",
		"слайд":                 "_____",
		"":                      "response",
		strings.Repeat("a", 70): strings.Repeat("a", 64),
	} {
		if got := schemaName(op); got != want {
			t.Errorf("schemaName(%q) = %q, kutilgan %q", op, got, want)
		}
	}
}
