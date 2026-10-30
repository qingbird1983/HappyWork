package jcs

import (
	"strings"
	"testing"
)

func TestCanonicalize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"key order", `{"b":1,"a":2}`, `{"a":2,"b":1}`},
		{"nested", `{"z":{"b":1,"a":2},"a":[3,{"y":1,"x":2}]}`, `{"a":[3,{"x":2,"y":1}],"z":{"a":2,"b":1}}`},
		{"negative zero", `{"n":-0}`, `{"n":0}`},
		{"small exponent", `{"n":0.0000001}`, `{"n":1e-7}`},
		{"large exponent", `{"n":1e21}`, `{"n":1e+21}`},
		{"integer exponent", `{"n":1e0}`, `{"n":1}`},
		{"unicode keys", `{"\u20ac":"euro","\r":"cr","\ufb33":"dalet","1":"one","\ud83d\ude00":"emoji","\u0080":"control","\u00f6":"o"}`, "{\"\\r\":\"cr\",\"1\":\"one\",\"\u0080\":\"control\",\"\u00f6\":\"o\",\"\u20ac\":\"euro\",\"\U0001F600\":\"emoji\",\"\uFB33\":\"dalet\"}"},
		{"escapes", `{"a":"\u0041\n\t"}`, `{"a":"A\n\t"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Canonicalize([]byte(tc.in))
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHashStableAcrossKeyOrder(t *testing.T) {
	a, err := Hash([]byte(`{"path":"a.txt","content":"hi","n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash([]byte(`{"content":"hi","n":1,"path":"a.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("hashes differ: %s %s", a, b)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct{ name, in string }{
		{"duplicate key", `{"a":1,"a":2}`},
		{"escaped duplicate", `{"a":1,"\u0061":2}`},
		{"nested duplicate", `{"x":[{"a":1,"a":2}]}`},
		{"big integer", `{"n":9007199254740993}`},
		{"huge exponent", `{"n":1e400}`},
		{"invalid utf8", "{\"a\":\"\xff\"}"},
		{"trailing garbage", `{"a":1} x`},
		{"two values", `{"a":1}{"b":2}`},
		{"empty", ``},
		{"bad json", `{"a":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate([]byte(tc.in)); err == nil {
				t.Fatalf("Validate accepted %q", tc.in)
			}
		})
	}
	if err := Validate([]byte(`{"n":9007199254740992}`)); err != nil {
		t.Fatalf("max exact integer rejected: %v", err)
	}
	if err := Validate([]byte(`{"n":1.5,"a":[true,false,null,"x"]}`)); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
}

func FuzzCanonicalizeStable(f *testing.F) {
	for _, seed := range []string{
		`{"b":1,"a":2}`, `{"n":-0}`, `{"n":1e21}`, `{"s":"\u20ac"}`,
		`{"a":[1,2,3]}`, `{"a":1,"a":2}`, `9007199254740992`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		out, err := Canonicalize(raw)
		if err != nil {
			return
		}
		again, err := Canonicalize(out)
		if err != nil {
			t.Fatalf("canonical output rejected: %v", err)
		}
		if string(again) != string(out) {
			t.Fatalf("not idempotent: %s -> %s", out, again)
		}
		if strings.ContainsAny(string(out), "\r\n") {
			t.Fatalf("canonical output contains raw newlines: %q", out)
		}
	})
}
