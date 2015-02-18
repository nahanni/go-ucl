# Go-UCL

##  Introduction

This is a parser and exporter for UCL fully implemented in Go. Refer to https://github.com/vstakhov/libucl for the UCL specification.

Currently it outputs to a `map[string] interface{}` after parsing. It may be improved in the future to output to a `struct` but in the meantime, either https://github.com/bitly/go-simplejson (simplify field access) or https://github.com/ld9999999999/go-interfacetools (copy map to struct) can be used to accomplish this task.

## License

This module is BSD-licensed; by Nahanni Systems Inc.
