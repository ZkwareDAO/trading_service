package persistence

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"
)

// csvMeta holds pre-computed CSV metadata for a struct type.
type csvMeta struct {
	headers []string
	indexes []int // field indexes in the struct
}

var (
	metaCacheMu sync.RWMutex
	metaCache   = make(map[reflect.Type]*csvMeta)
)

func buildMeta(t reflect.Type) *csvMeta {
	metaCacheMu.RLock()
	if meta, ok := metaCache[t]; ok {
		metaCacheMu.RUnlock()
		return meta
	}
	metaCacheMu.RUnlock()

	headers := make([]string, 0, t.NumField())
	indexes := make([]int, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("csv")
		if tag != "" && tag != "-" {
			headers = append(headers, tag)
			indexes = append(indexes, i)
		}
	}

	meta := &csvMeta{headers: headers, indexes: indexes}
	metaCacheMu.Lock()
	metaCache[t] = meta
	metaCacheMu.Unlock()
	return meta
}

// GetCSVHeaders returns the CSV column headers for the given struct.
func GetCSVHeaders(v interface{}) []string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return buildMeta(t).headers
}

// EncodeToCSVRow converts a struct to a CSV string slice.
func EncodeToCSVRow(v interface{}) ([]string, error) {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", val.Kind())
	}

	meta := buildMeta(val.Type())
	row := make([]string, len(meta.indexes))

	for i, fieldIdx := range meta.indexes {
		field := val.Field(fieldIdx)
		s, err := formatValue(field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", meta.headers[i], err)
		}
		row[i] = s
	}

	return row, nil
}

// formatValue converts a reflect.Value to its CSV string representation.
func formatValue(v reflect.Value) (string, error) {
	// Handle pointers (optional fields)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", nil
		}
		v = v.Elem()
	}

	switch kind := v.Kind(); kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		if v.Bool() {
			return "true", nil
		}
		return "false", nil
	default:
		// Check for time.Time
		if t, ok := v.Interface().(time.Time); ok {
			if t.IsZero() {
				return "", nil
			}
			return t.Format(time.RFC3339), nil
		}
		return "", fmt.Errorf("unsupported type: %s", kind)
	}
}
