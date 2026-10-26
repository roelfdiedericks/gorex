# Changelog

The version of each release is its heading here, which `mygo.json`'s
`version` must match: `make website` refuses to build otherwise, and
refuses a version already published.

## Unreleased

- A pane scrolls as one document with one scroll bar, the terminal's:
  a program's surface, such as omp's transcript, takes rows among the
  output, and a long one is whole and its end in reach. Scrollback is
  kept in lines at any width, as its setting says (it held a fraction
  of them in wide panes), up to 50,000; a transcript longer than the
  scrollback loses its start, as output does, never its end.
- Surfaces and omp's composer leave the scroll bar clear at the pane's
  edge.
- Scroll bar width, and Always show scroll bars, in Settings, Defaults:
  Appearance, for the terminal and everything else that scrolls.

## 0.1.1

- Programs that speak the Tern Surface Protocol, such as the coding
  agent omp, draw natively: their messages, tool calls, pickers,
  settings, forms, pictures, math and composer as GoRex's own controls,
  in their colors, among the terminal's output and kept in its
  scrollback. Turn it on for a host in Settings, Programs: Tern Surface
  Protocol (experimental); omp then starts natively by itself.
- A new icon: a rex in cyan on a dark tile.

## 0.1.0

The first release from gorex.goclaw.org.

- Tabs and split panes over sessions that outlive the app, kept by a
  session server; windows show each session through frames of the
  server's terminal, so a resize or a reattach never garbles a screen.
- Session servers on other hosts over ssh: GoRex copies its server there
  and reconnects when the link drops.
- An editor with a file tree, git changes, and a Markdown preview.
- Settings for the app and for each host.
- Images of the Kitty graphics protocol, pasting images into programs
  such as coding agents, and their program status (OSC 7501).
- A `gorex` command on the PATH from the Windows installer.
- Updates itself from gorex.goclaw.org: Check for Updates… in the command
  palette.
