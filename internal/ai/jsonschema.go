package ai

// jsonschema.go — AI javobi sxemasi uchun yagona ko'rinish: JSON Schema
// map[string]any shaklida. Bitta sxema barcha provayderlarga yuboriladi:
// Gemini'ga ResponseJsonSchema orqali (propertyOrdering qo'shilgan holda),
// OpenAI'ga Structured Outputs (response_format json_schema, strict) orqali.
//
// Strict rejim talabi: har bir obyekt barcha xossalarini "required" da
// sanaydi va "additionalProperties": false. Bo'sh bo'lishi mumkin bo'lgan
// maydonlar ham majburiy (promptlarda ishlatilmagan maydonlar bo'sh
// qoldirilishi aytilgan).

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// Object obyekt sxemasini yaratadi. keys — xossalar tartibi: model maydonlarni
// shu tartibda yozadi, u "required" da saqlanadi. keys da bo'lmagan xossalar
// oxiriga alifbo tartibida qo'shiladi, props da yo'q kalitlar tashlanadi —
// shuning uchun "required" doim barcha xossalarga teng.
func Object(props map[string]any, keys ...string) map[string]any {
	order := make([]string, 0, len(props))
	for _, k := range keys {
		if _, ok := props[k]; ok && !slices.Contains(order, k) {
			order = append(order, k)
		}
	}
	rest := make([]string, 0, len(props)-len(order))
	for k := range props {
		if !slices.Contains(order, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             append(order, rest...),
		"additionalProperties": false,
	}
}

// Array elementlari items sxemasiga mos massiv sxemasi.
func Array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

// String satr sxemasi.
func String() map[string]any { return map[string]any{"type": "string"} }

// Integer butun son sxemasi.
func Integer() map[string]any { return map[string]any{"type": "integer"} }

// Enum faqat values dagi qiymatlardan birini qabul qiladigan satr sxemasi.
func Enum(values []string) map[string]any {
	return map[string]any{"type": "string", "enum": slices.Clone(values)}
}

// stringArraySchema — satrlar massivi.
func stringArraySchema() map[string]any { return Array(String()) }

// withProperty obyekt sxemasiga oxirgi xossa sifatida yangi majburiy maydon qo'shadi.
func withProperty(s map[string]any, key string, prop map[string]any) {
	s["properties"].(map[string]any)[key] = prop
	s["required"] = append(s["required"].([]string), key)
}

// ---------------------------------------------------------------------------
// Provayderga moslash
// ---------------------------------------------------------------------------

// geminiSchema sxemaning nusxasini qaytaradi: har bir obyektga "required"
// tartibidagi "propertyOrdering" qo'shiladi. genai SDK so'rovni map orqali
// qayta yig'adi va kalitlar tartibi yo'qoladi; Gemini maydonlar tartibini shu
// nostandart xossadan oladi. Asl sxema o'zgarmaydi.
func geminiSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x)+1)
		for k, val := range x {
			out[k] = geminiSchema(val)
		}
		if props, ok := x["properties"].(map[string]any); ok {
			copied := make(map[string]any, len(props))
			for k, p := range props {
				copied[k] = geminiSchema(p)
			}
			out["properties"] = copied
			if req, ok := x["required"].([]string); ok {
				out["propertyOrdering"] = slices.Clone(req)
			}
		}
		return out
	case []string:
		return slices.Clone(x)
	default:
		return v
	}
}

// marshalSchema sxemani JSON qiladi; obyekt xossalari "required" tartibida
// yoziladi (encoding/json map kalitlarini alifbo bo'yicha saralaydi, OpenAI
// esa maydonlarni sxemadagi tartibda yozadi). Boshqa kalitlar — alifbo tartibida.
func marshalSchema(s map[string]any) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := encodeSchema(&buf, s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeSchema(buf *bytes.Buffer, v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}

	buf.WriteByte('{')
	for i, k := range sortedKeys(m) {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		buf.Write(key)
		buf.WriteByte(':')

		props, isProps := m[k].(map[string]any)
		if k != "properties" || !isProps {
			if err := encodeSchema(buf, m[k]); err != nil {
				return err
			}
			continue
		}
		order, _ := m["required"].([]string)
		buf.WriteByte('{')
		for j, name := range propertyOrder(props, order) {
			if j > 0 {
				buf.WriteByte(',')
			}
			key, _ := json.Marshal(name)
			buf.Write(key)
			buf.WriteByte(':')
			if err := encodeSchema(buf, props[name]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return nil
}

// propertyOrder xossalar nomlarini order tartibida, qolganlarini alifbo
// tartibida qaytaradi.
func propertyOrder(props map[string]any, order []string) []string {
	names := make([]string, 0, len(props))
	for _, k := range order {
		if _, ok := props[k]; ok && !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	for _, k := range sortedKeys(props) {
		if !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	return names
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// schemaName OpenAI json_schema nomini yasaydi: faqat [a-zA-Z0-9_-], 1..64
// belgi (boshqa belgilar "_" ga almashtiriladi).
func schemaName(op string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		}
		return '_'
	}, op)
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		return "response"
	}
	return name
}
