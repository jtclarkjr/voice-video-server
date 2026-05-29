package main

import (
	"slices"
	"testing"
)

func TestParseAllowedOriginsExpandsLoopbackHosts(t *testing.T) {
	origins := parseAllowedOrigins("http://localhost:5173,http://127.0.0.1:3000")

	for _, origin := range []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:3000",
		"http://localhost:3000",
	} {
		if !slices.Contains(origins, origin) {
			t.Fatalf("expected %q in %#v", origin, origins)
		}
	}
}

func TestParseAllowedOriginsRemovesDuplicates(t *testing.T) {
	origins := parseAllowedOrigins("http://localhost:5173,http://127.0.0.1:5173")

	if len(origins) != 2 {
		t.Fatalf("expected two unique origins, got %#v", origins)
	}
}
