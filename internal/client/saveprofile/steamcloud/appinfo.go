// Package steamcloud derives a game's own save locations from Steam's cloud
// configuration: the developer's own declaration of which folders Steam
// replicates, read out of appinfo.vdf.
//
// It exists because Steam Cloud's mirror under userdata is a transport and
// never a save (FDR-003, decision 10). A game whose community save-location
// rules do not apply to this Device still has one place its saves live, and
// for an Auto-Cloud game the developer already wrote that place down.
package steamcloud

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cache keeps each appinfo.vdf indexed between lookups. A scan asks about
// every installed game the manifest could not place, and Steam's cache is
// megabytes: reading and indexing it once per scan is the difference between
// that cost and paying it per game. A file Steam has rewritten since is
// indexed again.
type cache struct {
	mu    sync.Mutex
	files map[string]*appinfoFile
}

func newCache() *cache {
	return &cache{files: make(map[string]*appinfoFile)}
}

func (c *cache) read(path string) (*appinfoFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached := c.files[path]; cached != nil &&
		cached.modTime.Equal(info.ModTime()) && cached.size == info.Size() {
		return cached, nil
	}
	file, err := readAppinfo(path)
	if err != nil {
		return nil, err
	}
	c.files[path] = file
	return file, nil
}

// Steam's binary appinfo formats, by the version in the low byte of the
// magic. Only the newest holds its keys in a string table at the end of the
// file; the older two spell every key inline. Version 40 added the binary
// manifest's own digest to each app header, which the reader must step over
// to find where that app's key-values begin.
const (
	magicV39 = 0x07564427
	magicV40 = 0x07564428
	magicV41 = 0x07564429
)

// Binary key-values node types. Only the types Steam actually writes into
// appinfo are understood; anything else ends the read rather than guessing
// its width and desynchronizing everything after it.
const (
	typeNested = 0x00
	typeString = 0x01
	typeInt32  = 0x02
	typeFloat  = 0x03
	typeUint64 = 0x07
	typeEnd    = 0x08
)

// errMalformed reports an appinfo this reader cannot follow. Steam owns the
// format and may change it, so a game simply has no cloud configuration
// rather than a scan failing.
var errMalformed = errors.New("steamcloud: malformed appinfo")

// node is one branch of the parsed tree. Steam writes ordered lists as
// nested sections keyed "0", "1", …, so children carry their keys.
type node struct {
	values   map[string]string
	children map[string]*node
	order    []string
}

func newNode() *node {
	return &node{values: make(map[string]string), children: make(map[string]*node)}
}

// section returns a named child, or nil.
func (n *node) section(name string) *node {
	if n == nil {
		return nil
	}
	return n.children[strings.ToLower(name)]
}

// value returns a named scalar, or the empty string.
func (n *node) value(name string) string {
	if n == nil {
		return ""
	}
	return n.values[strings.ToLower(name)]
}

// sections returns the child sections in the order Steam wrote them, which
// is the order its numbered lists are meant to be read in.
func (n *node) sections() []*node {
	if n == nil {
		return nil
	}
	ordered := make([]*node, 0, len(n.children))
	for _, key := range n.order {
		if child := n.children[key]; child != nil {
			ordered = append(ordered, child)
		}
	}
	return ordered
}

// appinfoFile is one parsed-enough appinfo.vdf: its bytes, its key table if
// the format has one, and where each app's key-values begin. Apps are indexed
// but not parsed, so consulting one game costs a walk of fixed-size headers
// and the reading of one app.
type appinfoFile struct {
	data    []byte
	keys    []string
	bodies  map[uint32]appBody
	modTime time.Time
	size    int64
}

// appBody is where one app's key-values start and end.
type appBody struct{ from, to int }

// section reads one app's `appinfo` tree, or nil when this file has no such
// app.
func (f *appinfoFile) section(appID uint32) (*node, error) {
	body, known := f.bodies[appID]
	if !known {
		return nil, nil
	}
	tree, _, err := readNode(f.data[:body.to], body.from, f.keys)
	if err != nil {
		return nil, err
	}
	return tree.section("appinfo"), nil
}

// readAppinfo indexes an appinfo.vdf.
func readAppinfo(path string) (*appinfoFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, errMalformed
	}
	magic := binary.LittleEndian.Uint32(data)
	if magic != magicV39 && magic != magicV40 && magic != magicV41 {
		return nil, fmt.Errorf("%w: unknown format %#x", errMalformed, magic)
	}

	file := &appinfoFile{
		data:    data,
		bodies:  make(map[uint32]appBody),
		modTime: info.ModTime(),
		size:    info.Size(),
	}
	offset := 8
	if magic == magicV41 {
		if len(data) < 16 {
			return nil, errMalformed
		}
		file.keys, err = readStringTable(data, int64(binary.LittleEndian.Uint64(data[8:])))
		if err != nil {
			return nil, err
		}
		offset = 16
	}
	// Fixed app header: info state, last updated, PICS token, the text
	// manifest's SHA1, and the change number — then, from version 40, the
	// binary manifest's own SHA1.
	header := 8 + 4 + 4 + 8 + 20 + 4
	if magic != magicV39 {
		header += 20
	}
	for {
		// The list ends with an app id of zero and nothing after it, so the
		// terminator is read before a full entry header is required.
		if offset+4 > len(data) {
			return nil, errMalformed
		}
		appID := binary.LittleEndian.Uint32(data[offset:])
		if appID == 0 {
			return file, nil
		}
		if offset+8 > len(data) {
			return nil, errMalformed
		}
		size := int(binary.LittleEndian.Uint32(data[offset+4:]))
		next := offset + 8 + size
		if size < 0 || next > len(data) || offset+header > next {
			return nil, errMalformed
		}
		file.bodies[appID] = appBody{from: offset + header, to: next}
		offset = next
	}
}

func readStringTable(data []byte, offset int64) ([]string, error) {
	if offset < 0 || offset+4 > int64(len(data)) {
		return nil, errMalformed
	}
	position := int(offset)
	count := int(binary.LittleEndian.Uint32(data[position:]))
	position += 4
	// Every key costs at least its terminator, so a count larger than the
	// bytes left is a corrupt file rather than an allocation to attempt.
	if count < 0 || count > len(data)-position {
		return nil, errMalformed
	}
	table := make([]string, 0, count)
	for range count {
		end := indexByte(data, position)
		if end < 0 {
			return nil, errMalformed
		}
		table = append(table, string(data[position:end]))
		position = end + 1
	}
	return table, nil
}

// readNode reads one section and returns the offset just past its end marker.
func readNode(data []byte, position int, table []string) (*node, int, error) {
	current := newNode()
	for {
		if position >= len(data) {
			return nil, 0, errMalformed
		}
		kind := data[position]
		position++
		if kind == typeEnd {
			return current, position, nil
		}
		key, next, err := readKey(data, position, table)
		if err != nil {
			return nil, 0, err
		}
		position = next
		key = strings.ToLower(key)
		switch kind {
		case typeNested:
			child, after, err := readNode(data, position, table)
			if err != nil {
				return nil, 0, err
			}
			if _, exists := current.children[key]; !exists {
				current.order = append(current.order, key)
			}
			current.children[key] = child
			position = after
		case typeString:
			end := indexByte(data, position)
			if end < 0 {
				return nil, 0, errMalformed
			}
			current.values[key] = string(data[position:end])
			position = end + 1
		case typeInt32, typeFloat:
			if position+4 > len(data) {
				return nil, 0, errMalformed
			}
			current.values[key] = strconv.FormatUint(uint64(binary.LittleEndian.Uint32(data[position:])), 10)
			position += 4
		case typeUint64:
			if position+8 > len(data) {
				return nil, 0, errMalformed
			}
			current.values[key] = strconv.FormatUint(binary.LittleEndian.Uint64(data[position:]), 10)
			position += 8
		default:
			return nil, 0, fmt.Errorf("%w: unknown value type %#x", errMalformed, kind)
		}
	}
}

// readKey reads a key: an index into the string table, or an inline string
// in the oldest format, which has no table.
func readKey(data []byte, position int, table []string) (string, int, error) {
	if table == nil {
		end := indexByte(data, position)
		if end < 0 {
			return "", 0, errMalformed
		}
		return string(data[position:end]), end + 1, nil
	}
	if position+4 > len(data) {
		return "", 0, errMalformed
	}
	index := int(binary.LittleEndian.Uint32(data[position:]))
	if index < 0 || index >= len(table) {
		return "", 0, errMalformed
	}
	return table[index], position + 4, nil
}

func indexByte(data []byte, from int) int {
	for position := from; position < len(data); position++ {
		if data[position] == 0 {
			return position
		}
	}
	return -1
}
