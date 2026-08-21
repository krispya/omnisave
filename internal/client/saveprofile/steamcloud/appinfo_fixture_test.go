package steamcloud_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// appinfo builds a binary appinfo.vdf in Steam's newest format: a header, one
// entry per app, and the key table every key is an index into. Tests describe
// apps as nested maps of strings, which is all the reader takes from the real
// file.
type app struct {
	id      uint32
	section map[string]any
}

// writeAppinfo writes the newest format, whose keys live in a table.
func writeAppinfo(t *testing.T, directory string, apps ...app) string {
	t.Helper()
	return writeAppinfoVersion(t, directory, 0x07564429, apps...)
}

// writeAppinfoVersion writes one of Steam's formats. Version 41 keeps its
// keys in a table at the end of the file; version 40 spells them inline and
// has no table pointer in its header.
func writeAppinfoVersion(t *testing.T, directory string, magic uint32, apps ...app) string {
	t.Helper()
	if magic != 0x07564429 {
		return writeInlineAppinfo(t, directory, magic, apps...)
	}
	var keys []string
	index := map[string]uint32{}
	key := func(name string) uint32 {
		if known, exists := index[name]; exists {
			return known
		}
		index[name] = uint32(len(keys))
		keys = append(keys, name)
		return index[name]
	}

	var body []byte
	for _, entry := range apps {
		blob := writeSection(map[string]any{"appinfo": entry.section}, key)
		header := make([]byte, 8+4+4+8+20+4+20)
		binary.LittleEndian.PutUint32(header, entry.id)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(header)-8+len(blob)))
		body = append(body, header...)
		body = append(body, blob...)
	}
	body = append(body, 0, 0, 0, 0)

	table := make([]byte, 4)
	binary.LittleEndian.PutUint32(table, uint32(len(keys)))
	for _, name := range keys {
		table = append(table, append([]byte(name), 0)...)
	}

	file := make([]byte, 16)
	binary.LittleEndian.PutUint32(file, 0x07564429)
	binary.LittleEndian.PutUint32(file[4:], 1)
	binary.LittleEndian.PutUint64(file[8:], uint64(16+len(body)))
	file = append(file, body...)
	file = append(file, table...)

	path := filepath.Join(directory, "appinfo.vdf")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, file, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSection writes one nested section: string values, then subsections,
// each keyed by its index in the table.
func writeSection(values map[string]any, key func(string) uint32) []byte {
	var out []byte
	for _, name := range sortedKeys(values) {
		switch value := values[name].(type) {
		case string:
			out = append(out, 0x01)
			out = binary.LittleEndian.AppendUint32(out, key(name))
			out = append(out, append([]byte(value), 0)...)
		case int:
			out = append(out, 0x02)
			out = binary.LittleEndian.AppendUint32(out, key(name))
			out = binary.LittleEndian.AppendUint32(out, uint32(value))
		case map[string]any:
			out = append(out, 0x00)
			out = binary.LittleEndian.AppendUint32(out, key(name))
			out = append(out, writeSection(value, key)...)
		}
	}
	return append(out, 0x08)
}

func sortedKeys(values map[string]any) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	// Numbered sections must keep their order; Steam writes them ascending.
	for outer := 1; outer < len(names); outer++ {
		for inner := outer; inner > 0 && names[inner] < names[inner-1]; inner-- {
			names[inner], names[inner-1] = names[inner-1], names[inner]
		}
	}
	return names
}

// writeInlineAppinfo writes a pre-table format, where every key is spelled
// where it is used.
func writeInlineAppinfo(t *testing.T, directory string, magic uint32, apps ...app) string {
	t.Helper()
	inline := func(name string) uint32 { return 0 }
	var body []byte
	for _, entry := range apps {
		blob := writeInlineSection(map[string]any{"appinfo": entry.section}, inline)
		header := make([]byte, 8+4+4+8+20+4+20)
		binary.LittleEndian.PutUint32(header, entry.id)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(header)-8+len(blob)))
		body = append(body, header...)
		body = append(body, blob...)
	}
	body = append(body, 0, 0, 0, 0)

	file := make([]byte, 8)
	binary.LittleEndian.PutUint32(file, magic)
	binary.LittleEndian.PutUint32(file[4:], 1)
	file = append(file, body...)

	path := filepath.Join(directory, "appinfo.vdf")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, file, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInlineSection(values map[string]any, key func(string) uint32) []byte {
	var out []byte
	for _, name := range sortedKeys(values) {
		switch value := values[name].(type) {
		case string:
			out = append(out, 0x01)
			out = append(out, append([]byte(name), 0)...)
			out = append(out, append([]byte(value), 0)...)
		case int:
			out = append(out, 0x02)
			out = append(out, append([]byte(name), 0)...)
			out = binary.LittleEndian.AppendUint32(out, uint32(value))
		case map[string]any:
			out = append(out, 0x00)
			out = append(out, append([]byte(name), 0)...)
			out = append(out, writeInlineSection(value, key)...)
		}
	}
	return append(out, 0x08)
}
