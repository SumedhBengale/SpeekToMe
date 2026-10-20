// Package privacylog accepts constant diagnostic templates and only renders
// primitive numeric/boolean metadata. Strings, errors, and objects are private.
package privacylog

import (
	"fmt"
	"io"
	stdlog "log"
	"os"
	"reflect"
)

type Template string

func safe(value any) any {
	if value == nil {
		return "[omitted]"
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	default:
		return "[private]"
	}
}
func render(format Template, args ...any) string {
	values := make([]any, len(args))
	for i, value := range args {
		values[i] = safe(value)
	}
	return fmt.Sprintf(string(format), values...)
}
func Printf(format Template, args ...any) { stdlog.Print(render(format, args...)) }
func Print(args ...any)                   { stdlog.Print("[diagnostic event: values omitted]") }
func Println(args ...any)                 { Print(args...) }
func Fatalf(format Template, args ...any) { Printf(format, args...); os.Exit(1) }
func Fatal(args ...any)                   { Print(args...); os.Exit(1) }
func Fatalln(args ...any)                 { Fatal(args...) }

// HTTP's panic logger can contain arbitrary handler values and request URLs.
type privateWriter struct{}

func (privateWriter) Write(p []byte) (int, error) {
	stdlog.Print("[HTTP] handler or connection failure; details omitted")
	return len(p), nil
}
func HTTPLogger() *stdlog.Logger { return stdlog.New(privateWriter{}, "", 0) }

var _ io.Writer = privateWriter{}
