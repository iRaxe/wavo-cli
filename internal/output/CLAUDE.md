# internal/output - Wavo output rendering

`output.go` contains the CLI's output printer and text-sanitization helpers.
There are no separate printer implementations or struct-tag-driven renderers.

## Public interface

`Printer` has four exported fields:

- `Format string`: `text`, `json`, or `csv`; unknown values use text-table
  rendering in `PrintTable`.
- `Writer io.Writer`: destination for normal output; `NewPrinter` defaults it
  to `os.Stdout`.
- `NoColor bool`: carries the root `--no-color` setting. `output.go` does not
  itself add color.
- `Quiet bool`: changes `PrintTable` to emit only each nonempty row's first
  column.

Constructors and rendering methods:

- `NewPrinter(format string, noColor, quiet bool) *Printer`
- `(*Printer).PrintTable(headers []string, rows [][]string)`
- `(*Printer).PrintJSON(value interface{})`
- `(*Printer).PrintDetail(pairs []KeyValue)`

`KeyValue` contains exported `Key` and `Value` string fields. Rendering methods
do not return errors. JSON encoding and CSV flush failures are reported to
stderr; ordinary writer errors are otherwise best-effort.

Conceptual command-layer usage:

```go
package cmd

import (
  "io"

  "github.com/iRaxe/wavo-cli/internal/output"
)

func renderInbox(w io.Writer) {
  printer := output.NewPrinter("text", false, false)
  printer.Writer = w
  printer.PrintTable(
    []string{"ID", "Name"},
    [][]string{{"5", "Wavo Support"}},
  )
  printer.PrintDetail([]output.KeyValue{{Key: "ID", Value: "5"}})
}
```

## Rendering contracts

`PrintTable` selects behavior from `Printer`:

- Quiet mode takes precedence over `Format` and prints the sanitized first
  cell of each nonempty row, one per line.
- Text uses `tabwriter`, prints the supplied headers, and sanitizes every row
  cell with `SanitizeText`.
- JSON converts each row to a `map[string]string` keyed by the corresponding
  header, then delegates to `PrintJSON`.
- CSV writes the supplied headers and rows with `encoding/csv`. Row cells
  beginning with `=`, `+`, `-`, or `@` receive a leading apostrophe to prevent
  spreadsheet formula execution; headers are written as supplied.

`PrintJSON` pretty-prints the supplied value with two-space indentation. It is
the full-fidelity path used by commands that expose SDK response structures; it
does not consult `Quiet` or sanitize values into human text.

`PrintDetail` accepts explicit `[]KeyValue` pairs. In JSON mode it emits a
`map[string]string`. Otherwise it aligns keys and sanitizes values. It does not
consult `Quiet`.

Some conversation mutation commands implement their own quiet ID output in the
command layer. Do not infer that every printer method or detail view is silent
when `Quiet` is true.

## Sanitization

`SanitizeText` removes ANSI/control sequences, other Unicode control characters,
and invalid UTF-8 bytes from human-readable output. Text tables, text details,
and quiet table cells use it. Full-fidelity JSON deliberately preserves the
underlying data, while CSV applies formula escaping rather than terminal-text
sanitization.
