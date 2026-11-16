package lang

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestAllLabelsFilled(t *testing.T) {
	for _, l := range All() {
		if l.Button == "" || l.Name == "" || l.Prompt == "" || l.OOXML == "" {
			t.Errorf("%s: asosiy maydonlar bo'sh: %+v", l.Code, l)
		}
		v := reflect.ValueOf(l.Labels)
		for i := range v.NumField() {
			if v.Field(i).String() == "" {
				t.Errorf("%s: Labels.%s bo'sh", l.Code, v.Type().Field(i).Name)
			}
		}
		if got := fmt.Sprintf(l.Labels.YearFormat, "2026"); !strings.Contains(got, "2026") {
			t.Errorf("%s: YearFormat noto'g'ri: %q", l.Code, got)
		}
	}
}

func TestGetFallsBackToUzbekLatin(t *testing.T) {
	for _, code := range []string{"", "xx"} {
		if got := Get(code).Code; got != UzLatn {
			t.Errorf("Get(%q) = %s, want %s", code, got, UzLatn)
		}
	}
	if got := Get("ru").Labels.Introduction; got != "Введение" {
		t.Errorf("ru Introduction = %q", got)
	}
	if _, ok := Find(""); ok {
		t.Error(`Find("") topmasligi kerak`)
	}
}
