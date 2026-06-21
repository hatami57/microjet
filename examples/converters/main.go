// Command converters demonstrates MicroJet's generic conversion helpers:
// core/jsonx for JSON and struct<->map conversion, and core/utils for pointer
// coalescing. These are small, dependency-free utilities used throughout the
// framework.
//
// Run it with:
//
//	go run .
package main

import (
	"fmt"

	"github.com/hatami57/microjet/core/jsonx"
	"github.com/hatami57/microjet/core/utils"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func main() {
	u := User{ID: 7, Name: "Ada", Email: "ada@example.com"}

	// 1. ToJSON / FromJSON: type-safe JSON (de)serialization with generics, no
	// json.Marshal boilerplate and no interface{} juggling.
	s, _ := jsonx.ToJSON(u)
	back, _ := func() (User, error) { var v User; err := jsonx.FromJSON(s, &v); return v, err }()
	fmt.Println("== json round-trip ==")
	fmt.Printf("  ToJSON   = %s\n", s)
	fmt.Printf("  FromJSON = %+v\n", back)

	// 2. ToMap / MapTo: convert between a struct and a map[string]any (via JSON
	// tags). Useful for partial updates, dynamic payloads, and message envelopes.
	m, _ := jsonx.ToMap(u)
	fmt.Println("\n== struct <-> map ==")
	fmt.Printf("  ToMap = %v\n", m)
	m["name"] = "Grace"
	patched, _ := jsonx.MapTo[User](m)
	fmt.Printf("  MapTo (after edit) = %+v\n", patched)

	// 3. Coalesce / CoalesceVal: return the first non-nil pointer, the idiomatic
	// way to apply optional overrides over a default. CoalesceVal unwraps to a
	// concrete value with an explicit fallback.
	def := "default-region"
	var unset *string
	override := ptr("eu-west-1")
	fmt.Println("\n== coalesce ==")
	fmt.Printf("  first non-nil ptr  = %s\n", *utils.Coalesce(unset, override))
	fmt.Printf("  value with default = %s\n", utils.CoalesceVal(def, unset))
}

func ptr[T any](v T) *T { return &v }
