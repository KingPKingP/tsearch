package main

import "testing"

func TestParseFZFOutput(t *testing.T) {
	event := parseFZFOutput([]byte("query\nctrl-y\n1\tFILE\t/tmp/a.txt\n"))
	if !event.OK {
		t.Fatalf("expected event OK")
	}
	if event.Query != "query" || event.Key != "ctrl-y" || event.Path != "/tmp/a.txt" || event.Kind != kindFile {
		t.Fatalf("event = %#v", event)
	}
}
