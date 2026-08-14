// Package keyvalues reads Valve's binary KeyValues, the format Steam's client
// caches use on disk. Reading is deliberately forgiving: a caller navigates
// with Child and every missing, mistyped, or truncated branch answers as
// absent rather than as an error, so a cache Steam changed shape on degrades
// to "nothing to say" instead of failing its caller.
package keyvalues

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf16"
)

// Node types as written by Steam. Anything else ends the read.
const (
	typeObject  = 0
	typeString  = 1
	typeInt32   = 2
	typeFloat32 = 3
	typePointer = 4
	typeWString = 5
	typeColor   = 6
	typeUint64  = 7
	typeEnd     = 8
	typeInt64   = 10
	typeEndAlt  = 11
)

// maxDepth bounds nesting so a damaged file cannot drive the reader into
// unbounded recursion. Steam's caches nest four or five levels.
const maxDepth = 32

// Value is one node: either an object with named children or a leaf. The zero
// value and a nil *Value are both valid and read as absent, which is what lets
// callers chain Child without checking each step.
type Value struct {
	children map[string]*Value
	// keys preserves the file's order, which is the only order the achievement
	// bits in a schema are given in.
	keys   []string
	text   string
	number int64
	// numeric marks a leaf that carried an integer, so Int can tell a real 0
	// from an absent value.
	numeric bool
}

// Read parses one binary KeyValues document, reading at most limit bytes. The
// returned Value is the document root, whose children are the top-level
// objects — Steam writes exactly one, named for the app or the cache.
func Read(reader io.Reader, limit int64) (*Value, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return nil, fmt.Errorf("keyvalues: document exceeds %d bytes", limit)
	}
	root, _, err := readObject(data, 0, 0)
	if err != nil {
		return nil, err
	}
	return root, nil
}

// readObject parses children until an end marker or the end of the buffer,
// returning the offset just past what it consumed.
func readObject(data []byte, offset, depth int) (*Value, int, error) {
	if depth > maxDepth {
		return nil, 0, errors.New("keyvalues: document nests too deeply")
	}
	object := &Value{}
	for offset < len(data) {
		kind := data[offset]
		offset++
		if kind == typeEnd || kind == typeEndAlt {
			return object, offset, nil
		}
		key, next, err := readString(data, offset)
		if err != nil {
			return nil, 0, err
		}
		offset = next

		var child *Value
		switch kind {
		case typeObject:
			child, offset, err = readObject(data, offset, depth+1)
		case typeString:
			var text string
			text, offset, err = readString(data, offset)
			child = &Value{text: text}
		case typeWString:
			var text string
			text, offset, err = readWideString(data, offset)
			child = &Value{text: text}
		case typeInt32, typeColor, typePointer:
			var word []byte
			word, offset, err = readFixed(data, offset, 4)
			if err == nil {
				child = numberValue(int64(int32(binary.LittleEndian.Uint32(word))))
			}
		case typeFloat32:
			var word []byte
			word, offset, err = readFixed(data, offset, 4)
			if err == nil {
				number := math.Float32frombits(binary.LittleEndian.Uint32(word))
				child = &Value{text: strconv.FormatFloat(float64(number), 'g', -1, 32)}
			}
		case typeUint64, typeInt64:
			var word []byte
			word, offset, err = readFixed(data, offset, 8)
			if err == nil {
				child = numberValue(int64(binary.LittleEndian.Uint64(word)))
			}
		default:
			return nil, 0, fmt.Errorf("keyvalues: unsupported node type %d", kind)
		}
		if err != nil {
			return nil, 0, err
		}
		object.put(key, child)
	}
	// Steam's caches end with one more terminator than they open objects, so a
	// buffer that simply runs out is a complete document, not a damaged one.
	return object, offset, nil
}

func numberValue(number int64) *Value {
	return &Value{number: number, numeric: true, text: strconv.FormatInt(number, 10)}
}

func (v *Value) put(key string, child *Value) {
	if v.children == nil {
		v.children = make(map[string]*Value)
	}
	if _, exists := v.children[key]; !exists {
		v.keys = append(v.keys, key)
	}
	v.children[key] = child
}

func readString(data []byte, offset int) (string, int, error) {
	for index := offset; index < len(data); index++ {
		if data[index] == 0 {
			return string(data[offset:index]), index + 1, nil
		}
	}
	return "", 0, errors.New("keyvalues: unterminated string")
}

func readWideString(data []byte, offset int) (string, int, error) {
	for index := offset; index+1 < len(data); index += 2 {
		if data[index] == 0 && data[index+1] == 0 {
			units := make([]uint16, 0, (index-offset)/2)
			for at := offset; at < index; at += 2 {
				units = append(units, binary.LittleEndian.Uint16(data[at:]))
			}
			return string(utf16.Decode(units)), index + 2, nil
		}
	}
	return "", 0, errors.New("keyvalues: unterminated wide string")
}

func readFixed(data []byte, offset, width int) ([]byte, int, error) {
	if offset+width > len(data) {
		return nil, 0, errors.New("keyvalues: truncated value")
	}
	return data[offset : offset+width], offset + width, nil
}

// Child walks the named path, answering nil for any step that is missing or
// is a leaf where an object was expected.
func (v *Value) Child(path ...string) *Value {
	current := v
	for _, key := range path {
		if current == nil {
			return nil
		}
		current = current.children[key]
	}
	return current
}

// Keys lists an object's child names in the order the file wrote them. A leaf
// or an absent value has none.
func (v *Value) Keys() []string {
	if v == nil {
		return nil
	}
	return v.keys
}

// Text returns a leaf's string form, and "" for an object or an absent value.
func (v *Value) Text() string {
	if v == nil {
		return ""
	}
	return v.text
}

// Int returns a leaf's integer value. Only a node that carried an integer
// reports true, so an absent value and a stored zero stay distinguishable.
func (v *Value) Int() (int64, bool) {
	if v == nil || !v.numeric {
		return 0, false
	}
	return v.number, true
}
