## 2026-11-01 - INI section and line injection via CRLF in structured file generators
**Vulnerability:** Structured text generators (such as INI, YAML, or XML writers) receiving string fields from JSON endpoints must sanitize line terminators (`\r`, `\n`) and syntactic delimiters (e.g. `[` and `]`) in additions and updates.
**Learning:** Even when field separators like `=` and `|` were replaced, unescaped newlines in JSON payloads allowed injecting entire sections and arbitrary key-value pairs into the generated `.ini` file.
**Prevention:** Always strip or sanitize carriage returns, newlines, and section delimiter brackets when serializing user-supplied JSON data to line-based config formats.
