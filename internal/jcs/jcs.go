// Package jcs canonicalizes JSON arguments for the execution lock (RFC 8785)
// and rejects inputs outside the I-JSON subset before hashing (ADR-0012).
package jcs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// maxExactInt is the largest integer a float64 represents exactly (2^53).
var maxExactInt = big.NewInt(1 << 53)

// Validate checks that raw is UTF-8 JSON in the I-JSON subset: one top-level
// value, no duplicate object keys (after escape decoding), and numbers that
// survive IEEE-754 conversion without silently changing an integer.
func Validate(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("jcs: input is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := validateValue(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("jcs: trailing JSON value")
		}
		return fmt.Errorf("jcs: trailing data: %w", err)
	}
	return nil
}

func validateValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("jcs: invalid JSON: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		if num, ok := tok.(json.Number); ok {
			return validateNumber(num)
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return fmt.Errorf("jcs: object key: %w", err)
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("jcs: object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("jcs: duplicate object key %q", key)
			}
			seen[key] = true
			if err := validateValue(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("jcs: unterminated object: %w", err)
		}
	case '[':
		for dec.More() {
			if err := validateValue(dec); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("jcs: unterminated array: %w", err)
		}
	default:
		return fmt.Errorf("jcs: unexpected delimiter %q", delim)
	}
	return nil
}

func validateNumber(num json.Number) error {
	s := num.String()
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("jcs: number %q is not a finite IEEE-754 value", s)
		}
		return nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return fmt.Errorf("jcs: invalid number %q", s)
	}
	abs := new(big.Int).Abs(n)
	if abs.Cmp(maxExactInt) > 0 {
		return fmt.Errorf("jcs: integer %q exceeds the exact float64 range", s)
	}
	return nil
}

// Canonicalize validates and returns the RFC 8785 canonical bytes.
func Canonicalize(raw []byte) ([]byte, error) {
	if err := Validate(raw); err != nil {
		return nil, err
	}
	out, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("jcs: canonicalize: %w", err)
	}
	return out, nil
}

// Hash returns the SHA-256 of the canonical form as lowercase hex.
func Hash(raw []byte) (string, error) {
	canonical, err := Canonicalize(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
