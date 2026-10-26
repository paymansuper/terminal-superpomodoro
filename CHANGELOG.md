# Changelog

## v0.3.3

- Mini window: the buttons of the big window that appears when a phase ends are readable again, on any theme and in light mode
- The window has a solid dark backing and always fits its content, so nothing shows through or gets cut off
- Several running timers no longer share one mini window file, which left old windows stacked behind the new one

## v0.3.2

- Filled background pictures can be moved up, down, left and right to choose the visible part (`image_shift_y`, `image_shift_x`), by dragging the preview or with sliders in the theme editor, also in the mini window
- Uploaded pictures can be deleted in the theme editor, with a warning when a theme still uses them
- Themes can be removed: own themes are deleted, built-in themes are taken out of the list and can be restored, in the theme editor and with `spomo theme rm` and `spomo theme restore`

## v0.3.1

- Focus sessions can hold several tasks: `return` in the list adds a task to the session or takes it out
- Session tasks show under the timer and as a clickable checklist in the mini window, also in the big window when a phase ends
- When a focus phase ends every session task gets the pomodoro, done tasks leave the session, open ones carry over

## v0.3.0

- To-do list in the timer (`l`): add, edit, reorder, mark done and focus on a task, pomodoros are counted per task, stored as a Markdown checklist
- `spomo todo` command to manage the list from the shell
- Current task shows under the timer and in the mini window
- Theme, font, bar and scene open a picker with live preview instead of cycling
- Seven built-in themes (tokyo-night, catppuccin, dracula, kanagawa, synthwave, matrix, mono), Nord, Gruvbox, Rosé Pine and Everforest moved to `examples/themes` as importable `.spomotheme` files
- `esc` no longer quits, it closes panels, quit with `q`

## v0.2.2

- Background images can be shown whole (`image_fit = "contain"`) with size and position, logos and transparent pictures do that automatically
- Transparent parts of pictures stay see through, in the terminal, the theme editor and the mini window
- Smoother scaling for sharp pictures
- Theme editor: fit, size and position controls and a sharp or pixel preview

## v0.2.1

- Mini window: Ghostty, Kitty, WezTerm, Alacritty and Warp now get out of the way when you press `w` (hidden like ⌘H, since they can't minimize on request)

## v0.2.0

- Sharp, full resolution background images in Ghostty, Kitty and iTerm2, pixel style stays as fallback
- New `image_mode` setting (`auto`, `sharp`, `pixel`)

## v0.1.0

First public release.

- Terminal Pomodoro timer with focus, short and long breaks and configurable rounds
- Eleven themes, six digit fonts and five progress bar styles
- Animated scenes for synthwave, matrix and kanagawa
- Floating mini window for macOS that comes to the center when a phase ends
- Sounds, desktop notifications and daily stats
- Automatic contrast adjustment for any terminal background
- Theme editor in the browser (`spomo themegen`) with live preview, palette generator and image upload
- Custom theme files with live reload and `spomo theme new`
- `.spomotheme` exchange format with import and export in the terminal and the theme editor
- PNG, JPG and GIF background images
- Homebrew formula with the `spomo` alias
