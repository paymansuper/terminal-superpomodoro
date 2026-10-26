# The `.spomotheme` format

SuperPomodoro keeps your themes as small `.theme` text files in `~/.config/superpomodoro/themes`. To share themes it uses a single, self contained exchange format with the extension `.spomotheme`.

A `.spomotheme` file is UTF-8 JSON. It can hold one theme or a whole collection. Every theme in it is complete on its own, which means

- all colors are fully resolved, the receiver doesn't need the theme it was based on
- the background image, if any, is embedded as base64, so nothing else needs to be sent along

The formal definition is the JSON Schema in [`theme-format.schema.json`](theme-format.schema.json).

## Example

```json
{
  "format": "superpomodoro-theme",
  "version": 1,
  "generator": "superpomodoro 0.1.0",
  "exported": "2026-10-26T11:18:43Z",
  "themes": [
    {
      "name": "fjord",
      "description": "Calm blues for long deep work sessions",
      "author": "Payman",
      "extends": "nord",
      "colors": {
        "focus": ["#5e81ac", "#88c0d0"],
        "break": ["#a3be8c", "#ebcb8b"],
        "long": ["#b48ead", "#88c0d0"],
        "text": "#eceff4",
        "muted": "#4c566a",
        "surface": "#3b4252"
      },
      "scene": "waves",
      "image": {
        "file": "fjord.jpg",
        "brightness": 0.4,
        "fit": "cover",
        "data": "iVBORw0KGgoAAAANSUhEUgAA..."
      }
    }
  ]
}
```

## Fields

| Field | Required | Meaning |
| --- | :---: | --- |
| `format` | ✓ | Always `superpomodoro-theme` |
| `version` | ✓ | Format version, currently `1` |
| `generator` | | Program and version that wrote the file |
| `exported` | | Time of export, RFC 3339 |
| `themes` | ✓ | One or more themes |
| `themes[].name` | ✓ | Lowercase letters, digits and dashes, up to 32 characters |
| `themes[].description` | | Short description, up to 200 characters |
| `themes[].author` | | Author, up to 80 characters |
| `themes[].extends` | | The theme it was based on, for information only |
| `themes[].colors.focus` `break` `long` | ✓ | Gradient pair `["#rrggbb", "#rrggbb"]` for that phase |
| `themes[].colors.text` `muted` `surface` | ✓ | Interface text, secondary text, subtle decoration |
| `themes[].scene` | | `off`, `rain`, `waves` or `grid`, default `off` |
| `themes[].image.file` | | Original file name, png, jpg or gif |
| `themes[].image.brightness` | | 0 to 1, how bright the image is drawn |
| `themes[].image.fit` | | `auto`, `cover` (fill and crop) or `contain` (whole picture), default `auto` |
| `themes[].image.scale` | | Size for `contain`, 0 to 1 of the window |
| `themes[].image.position` | | `top`, `center` or `bottom` for `contain` |
| `themes[].image.data` | | The image file as base64, at most 20 MB |

## Import rules

- A file with an unknown `format` or a newer `version` is rejected with a clear message.
- The whole file is validated before anything is written, a broken file never imports halfway.
- Names are sanitized. If a name is already taken, by your own theme or a built-in one, the imported theme gets a suffix like `fjord-2`. `--force` overwrites instead.
- Embedded images are checked to be real png, jpg or gif files and stored in `themes/images`. An identical picture that already exists is reused.
- `extends` is kept only if the receiver has that theme, it never changes the colors.
- Plain `.theme` text files can be imported the same way.

## Versioning

Readers must reject versions they don't know. Additions that older readers can safely ignore don't need a new version. Changes in meaning do.
