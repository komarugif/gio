# Gio for KomaruGram

This is a fork of [Gio](https://gioui.org) v0.10.2 that
[KomaruGram](https://github.com/komarugif/komarugram-go), a Telegram desktop
client in Go, is built on. The module path is still `gioui.org`, so the
client picks it up with a `replace` directive and no import changes.

The fork exists because the messenger needs things upstream Gio does not have
(yet). It is not meant to replace Gio for other programs, but nothing here
stops you from using it. The full, detailed list is in
[LOCAL_CHANGES.md](LOCAL_CHANGES.md); in short:

- **Windows and the desktop.** Transparent windows with blur behind them
  (Wayland, macOS, Windows), a dark or light system frame that follows the
  program's theme, window icons, raising a window from a tray icon, hidden
  Wayland windows that give their GPU memory back, and fixes for windows that
  were placed, suspended or destroyed at an awkward moment.
- **Keyboard and mouse.** Shortcuts such as Ctrl+C work on non-Latin layouts,
  the layout switch on Wayland is read correctly, a mouse wheel is told apart
  from a touchpad, and X11 gets smooth scrolling through XInput 2.1.
- **Clipboard and drag and drop.** Pictures, rich HTML and files can be copied
  and pasted, and files can be dragged in from other programs.
- **Text and emoji.** Color emoji (COLR v0 and v1, or pictures from an emoji
  pack) in regular, bold, italic and monospace text, emoji that follow the
  font the user picked, and extra metrics for laying text out.
- **Drawing.** Blur layers for floating panels, and a way to tell whether
  windows are drawn by software.
- **More platforms.** A driver for Haiku and support for Windows 7, which
  upstream does not have.
- **Robustness.** An editor, a list or the input router no longer stay broken
  after a panic in an earlier frame, so the window can recover and draw again.

Bug reports about these changes are welcome here; for everything else, see the
upstream project below.

---

# Gio - https://gioui.org

Immediate mode GUI programs in Go for Android, iOS, macOS, Linux,
FreeBSD, OpenBSD, Windows, and WebAssembly (experimental).

# Installation, examples, documentation

Go to [gioui.org](https://gioui.org).

[![builds.sr.ht status](https://builds.sr.ht/~eliasnaur/gio.svg)](https://builds.sr.ht/~eliasnaur/gio)

## Issues

File bugs and TODOs through the [issue tracker](https://todo.sr.ht/~eliasnaur/gio) or send an email
to [~eliasnaur/gio@todo.sr.ht](mailto:~eliasnaur/gio@todo.sr.ht). For general discussion, use the
mailing list: [~eliasnaur/gio@lists.sr.ht](mailto:~eliasnaur/gio@lists.sr.ht).

## Contributing

Post discussion to the [mailing list](https://lists.sr.ht/~eliasnaur/gio) and patches to
[gio-patches](https://lists.sr.ht/~eliasnaur/gio-patches). No Sourcehut
account is required and you can post without being subscribed.

See the [contribution guide](https://gioui.org/doc/contribute) for more details.

An [official GitHub mirror](https://github.com/gioui/gio) is available.

## Tags

Pre-1.0 tags are provided for reference only, and do not designate releases with ongoing support. Bugfixes will not be backported to older tags.

Tags follow semantic versioning. In particular, as the major version is zero:

- breaking API or behavior changes will increment the *minor* version component.
- non-breaking changes will increment the *patch* version component.
